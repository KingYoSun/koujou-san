import { expect, it } from "vitest";
import { publicationIssue } from "./publications";

it("follows the fixed Issue recorded by Hopper without substituting an unrelated Scope", () => {
  const issue = { id: "scope:hopper-accepted", version: "fixed-scope" };
  const value = {
    actor: {
      kind: "automation",
      user_id: "member",
      control: { id: "control:hopper", version: "fixed-control" },
    },
    input: {
      name: "synthetic",
      connection: "owned",
      title: "synthetic",
      body: "",
      sources: [{ id: "scope:other", version: "fixed-other" }, issue],
    },
    draft: { id: "publication-draft:one", version: "fixed-draft" },
    state: "unknown",
    reason: "response_lost",
    external_body: "",
    run_id: "run:one",
    connection: { repository: "fixture/repo" },
  };
  expect(publicationIssue("publication:hopper-accepted", value)).toEqual(issue);
  expect(
    publicationIssue("publication:hopper-unrelated", value),
  ).toBeUndefined();
  expect(
    publicationIssue("publication:hopper-accepted", {
      ...value,
      input: { ...value.input, sources: [{ ...issue, version: "head" }] },
    }),
  ).toBeUndefined();
  expect(
    publicationIssue("publication:hopper-accepted", {
      ...value,
      actor: { ...value.actor, kind: "user" },
    }),
  ).toBeUndefined();
});
