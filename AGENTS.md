This is a learning project for the developer. Make sure to explain concepts as needed without overcomplicating things.

If the user uses terms or jargon incorrectly during conversation, be sure to correct them by giving them the correct terminology.

Try to give pseudocode to the user first. Only give code for something if the user asks for it. Don't offer it voluntarily. 

This project uses TDD (Test Driven Development). TDD is a workflow where you write a test before writing the production code.
  The usual cycle is:
  1. Red: Write a test describing the desired behavior and confirm it fails.
  2. Green: Write the smallest amount of code needed to pass the test.
  3. Refactor: Improve the code while keeping all tests passing.

## Testing criteria

Test a behavior when its failure would matter and the behavior is not already
protected at a cheaper level. TDD does not require a dedicated test for every
function or line of production code.

Before adding a test, confirm that it protects:

1. A meaningful consequence, such as an incorrect API response, corrupted shared
   state, a resource leak, broken media, or a race.
2. A distinct behavior or failure mode that existing tests do not already cover.
3. A stable contract rather than a current implementation detail.
4. The behavior at the cheapest useful level: prefer a unit test unless an
   integration test is necessary to exercise the real interaction.
5. A clear signal: when the test fails, it should identify the behavior that
   regressed.

Add tests for public and API contracts, state and ownership invariants, cleanup
and important failure recovery, concurrency guarantees, media or signaling
transformations, and bug regressions.

Usually omit tests for trivial plumbing, getters or assignments with no behavioral
consequence, implementation details, equivalent variations of an already-covered
input class, behavior adequately covered elsewhere, and third-party library
internals. Use table-driven tests for representative boundaries and input classes
instead of testing every possible value.
