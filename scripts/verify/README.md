# Verification helper integration

AR-003 has no contract or code generator sources yet. The generated drift gate
therefore checks all tracked, changed, deleted, and untracked paths for
common generated names (generated, gen, *_gen.*, *_generated.*, *.pb.*, and
mock/zz_generated forms). Any such artifact fails closed while the generator
manifest is empty.

AR-005 must add each deterministic generation command to
scripts/verify/generators.json before introducing generated artifacts. Each
entry has this shape:

~~~
{
  "name": "openapi",
  "command": "tool",
  "args": ["generate", "--input", "contracts/openapi.yaml"],
  "cwd": "."
}
~~~

The command runs from the repository checkout. Its working directory must stay
inside the checkout. task check-generated reruns every registered generator and
fails if any generated output differs from HEAD; a clean checkout alone is not
treated as proof of regeneration equivalence.
