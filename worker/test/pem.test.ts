import { generateKeyPairSync, webcrypto } from "node:crypto";
import { describe, expect, it } from "vitest";
import { pemToPkcs8 } from "../src/pem";

const { privateKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const pkcs1Pem = privateKey.export({ type: "pkcs1", format: "pem" }) as string;
const pkcs8Pem = privateKey.export({ type: "pkcs8", format: "pem" }) as string;
const pkcs8Der = new Uint8Array(
  privateKey.export({ type: "pkcs8", format: "der" }) as Buffer,
);

describe("pemToPkcs8", () => {
  it("passes through a pkcs8 key", () => {
    expect(pemToPkcs8(pkcs8Pem)).toEqual(pkcs8Der);
  });

  it("rewraps a pkcs1 key into pkcs8", () => {
    expect(pemToPkcs8(pkcs1Pem)).toEqual(pkcs8Der);
  });

  it("accepts escaped newlines", () => {
    expect(pemToPkcs8(pkcs1Pem.replace(/\n/g, "\\n"))).toEqual(pkcs8Der);
  });

  it("produces a key importable as RSASSA-PKCS1-v1_5", async () => {
    await expect(
      webcrypto.subtle.importKey(
        "pkcs8",
        pemToPkcs8(pkcs1Pem),
        { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
        false,
        ["sign"],
      ),
    ).resolves.toBeDefined();
  });
});
