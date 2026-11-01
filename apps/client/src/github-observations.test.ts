import { describe, expect, it } from "vitest";
import { sourceURL } from "./github-observations";

describe("external source instance", () => {
  it("keeps identical repository paths separate by explicit instance", () => {
    const source = "https://localhost:58109/proof/private/issues/1";
    expect(sourceURL(source, "https://localhost:58109")?.href).toBe(source);
    expect(sourceURL(source, "https://localhost:58110")).toBeUndefined();
    expect(
      sourceURL(
        "http://192.168.1.2:3000/git/proof/private",
        "http://192.168.1.2:3000/git",
      ),
    ).toBeDefined();
    expect(
      sourceURL("https://localhost/git-other/proof", "https://localhost/git"),
    ).toBeUndefined();
  });
  it("rejects foreign protocols and credentials and preserves the GitHub default", () => {
    expect(sourceURL("https://github.com/owner/repo/issues/1")).toBeDefined();
    expect(sourceURL("https://other.test/owner/repo/issues/1")).toBeUndefined();
    expect(sourceURL("file:///C:/Windows", "file:///C:/")).toBeUndefined();
    expect(
      sourceURL("https://key@localhost:58109/proof", "https://localhost:58109"),
    ).toBeUndefined();
  });
});
