import { describe, expect, it } from "vitest";
import { parseTarget, targetKey } from "../src/github";
import { HttpError } from "../src/http";

describe("target helpers", () => {
  it("parses and normalizes org target", () => {
    const target = parseTarget({
      type: "org",
      org: "My-Org",
      runner_group_id: 1,
    });
    expect(target.org).toBe("my-org");
    expect(targetKey(target)).toBe("org:my-org:1");
  });

  it("rejects invalid target", () => {
    expect(() => parseTarget({ type: "repo" })).toThrow(HttpError);
  });
});
