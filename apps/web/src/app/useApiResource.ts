import { useCallback, useEffect, useState } from "react";

import { ApiRequestError, type ApiFailureKind } from "./api";

export type ResourceState =
  | "loading"
  | "ready"
  | "empty"
  | "stale"
  | ApiFailureKind;

interface ResourceResult<T> {
  data?: T;
  error?: Error;
  state: ResourceState;
  retry: () => void;
}

export function useApiResource<T>(
  load: (signal: AbortSignal) => Promise<T>,
  isEmpty: (data: T) => boolean,
): ResourceResult<T> {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<Omit<ResourceResult<T>, "retry">>({
    state: "loading",
  });

  useEffect(() => {
    const controller = new AbortController();
    setResult((previous) => ({
      data: previous.data,
      state: "loading",
    }));

    void load(controller.signal)
      .then((data) => {
        setResult({ data, state: isEmpty(data) ? "empty" : "ready" });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        const apiError = error instanceof ApiRequestError ? error : undefined;
        setResult((previous) => ({
          data: previous.data,
          error:
            error instanceof Error ? error : new Error("Unknown API error"),
          state: previous.data ? "stale" : (apiError?.kind ?? "error"),
        }));
      });

    return () => controller.abort();
  }, [attempt, isEmpty, load]);

  const retry = useCallback(() => setAttempt((value) => value + 1), []);
  return { ...result, retry };
}
