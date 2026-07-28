import { describe, expect, it } from "vitest";
import { atobUrl, base64url, decodeBase64Url } from "../src/encoding";

describe("encoding", () => {
  it("round-trips base64url strings", () => {
    const input = "hello world";
    const encoded = base64url(input);
    expect(atobUrl(encoded)).toBe(input);
    expect(new TextDecoder().decode(decodeBase64Url(encoded))).toBe(input);
  });
});
