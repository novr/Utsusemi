import { describe, expect, it } from "vitest";
import { parseTarget, targetKey } from "../src/github";

describe("target helpers", () => {
  it("parses org target", () => {
    const target = parseTarget({
      type: "org",
      org: "my-org",
      runner_group_id: 1,
    });
    expect(targetKey(target)).toBe("org:my-org:1");
  });
});
