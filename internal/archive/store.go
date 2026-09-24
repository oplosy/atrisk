// Package archive writes immutable, content-addressed source payloads.
package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

var (
	ErrInvalidDigest   = errors.New("invalid content SHA-256")
	ErrContentMismatch = errors.New("existing archive object content does not match its address")
)

type Object struct {
	Key           string
	ContentSHA256 string
	MediaType     string
	Body          []byte
	Metadata      map[string]string
}

type Reference struct {
	Key           string
	ContentSHA256 string
	ByteLength    int64
	MediaType     string
}

// Store is deliberately small so ingestion can be tested without a network.
type Store interface {
	Put(ctx context.Context, object Object) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type S3Store struct {
	client s3API
	bucket string
}

func NewS3Store(client s3API, bucket string) (*S3Store, error) {
	if client == nil {
		return nil, errors.New("S3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("S3 bucket is required")
	}
	return &S3Store{client: client, bucket: bucket}, nil
}

func (s *S3Store) Put(ctx context.Context, object Object) error {
	if err := validateObject(object); err != nil {
		return err
	}
	metadata := SanitizedMetadata(object.Metadata)
	metadata["sha256"] = object.ContentSHA256

	// The HEAD makes retries safe even for S3 implementations that ignore
	// If-None-Match. The conditional PUT closes the normal race.
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(object.Key)})
	if err == nil {
		return s.verifyExisting(ctx, object, head)
	}
	if !isNotFound(err) {
		return fmt.Errorf("head archive object: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(object.Key), Body: bytes.NewReader(object.Body),
		ContentType: aws.String(object.MediaType), Metadata: metadata, IfNoneMatch: aws.String("*"),
	})
	if err == nil {
		return nil
	}
	if isConditionalConflict(err) {
		winner, headErr := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(object.Key)})
		if headErr == nil {
			return s.verifyExisting(ctx, object, winner)
		}
	}
	return fmt.Errorf("put archive object: %w", err)
}

func (s *S3Store) verifyExisting(ctx context.Context, object Object, head *s3.HeadObjectOutput) error {
	if head == nil || aws.ToInt64(head.ContentLength) != int64(len(object.Body)) || aws.ToString(head.ContentType) != object.MediaType {
		return ErrContentMismatch
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(object.Key)})
	if err != nil {
		return fmt.Errorf("get existing archive object: %w", err)
	}
	if result == nil || result.Body == nil {
		return ErrContentMismatch
	}
	defer result.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Body, int64(len(object.Body))+1))
	if err != nil {
		return fmt.Errorf("read existing archive object: %w", err)
	}
	if !bytes.Equal(body, object.Body) {
		return ErrContentMismatch
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validObjectKey(key) {
		return nil, fmt.Errorf("invalid archive object key %q", key)
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get archive object: %w", err)
	}
	return result.Body, nil
}

func ArchivePayload(ctx context.Context, store Store, body []byte, mediaType string, metadata map[string]string) (Reference, error) {
	if store == nil {
		return Reference{}, errors.New("archive store is required")
	}
	digest := SHA256Hex(body)
	ref := Reference{Key: ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: mediaType}
	if err := store.Put(ctx, Object{Key: ref.Key, ContentSHA256: digest, MediaType: mediaType, Body: body, Metadata: SanitizedMetadata(metadata)}); err != nil {
		return Reference{}, err
	}
	return ref, nil
}

func SHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func ObjectKey(digest string) string {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if !validDigest(digest) {
		return ""
	}
	return "sha256/" + digest[:2] + "/" + digest
}

func validateObject(object Object) error {
	if !validDigest(object.ContentSHA256) || ObjectKey(object.ContentSHA256) != object.Key {
		return ErrInvalidDigest
	}
	if SHA256Hex(object.Body) != object.ContentSHA256 {
		return ErrContentMismatch
	}
	if strings.TrimSpace(object.MediaType) == "" {
		return errors.New("archive media type is required")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

var keyPattern = regexp.MustCompile(`^sha256/[0-9a-f]{2}/[0-9a-f]{64}$`)

func validObjectKey(value string) bool { return keyPattern.MatchString(value) }

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NotFound" || code == "NoSuchKey" || code == "404"
	}
	return false
}

func isConditionalConflict(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "PreconditionFailed" || code == "ConditionalRequestConflict" || code == "412" || code == "409"
	}
	return false
}

var secretName = regexp.MustCompile(`(?i)(authorization|api[-_]?key|access[-_]?key|secret|token|password|credential|cookie|session)`)

// SanitizedMetadata removes credential-bearing fields and control characters.
func SanitizedMetadata(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || secretName.MatchString(key) {
			continue
		}
		var clean strings.Builder
		for _, r := range value {
			if unicode.IsControl(r) {
				continue
			}
			clean.WriteRune(r)
		}
		value := clean.String()
		if strings.Contains(key, "uri") || key == "url" {
			value = RedactedURL(value)
		}
		output[key] = value
	}
	return output
}

// RedactedURL preserves the path while removing credential-bearing query and
// user-info values before a URL is written to archive metadata or logs.
func RedactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid-url]"
	}
	u.User = nil
	query := u.Query()
	for key := range query {
		if secretName.MatchString(key) {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}
