# safety-regression-evaluator

## Objective
Contribute only the frozen policy-optimization artifact for this skill.

## Applicable Input
Use only the supplied bounded context and referenced immutable artifacts.

## Procedure
Follow the declared deterministic or model-backed executor contract.

## Mandatory Checks
Validate all references, hashes, closed enums, and output schema fields.

## Prohibitions
Do not approve, release, normalize trusted data, invent IDs, or read undeclared payloads.

## Output Contract
Return exactly one JSON object matching output.schema.json.

## Failure Handling
Return a safe error category and never convert failure into success.
