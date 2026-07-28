const PEM_ARMOR = /-----(?:BEGIN|END)[^-]*-----/g;
const PKCS1_HEADER = "BEGIN RSA PRIVATE KEY";

// AlgorithmIdentifier for rsaEncryption (1.2.840.113549.1.1.1) with NULL parameters.
const RSA_ALGORITHM_IDENTIFIER = new Uint8Array([
  0x30, 0x0d, 0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x01, 0x01,
  0x05, 0x00,
]);
const PKCS8_VERSION = new Uint8Array([0x02, 0x01, 0x00]);

const SEQUENCE = 0x30;
const OCTET_STRING = 0x04;

/**
 * GitHub hands out App private keys as PKCS#1, but WebCrypto only imports
 * PKCS#8, so PKCS#1 keys are rewrapped instead of rejected.
 */
export function pemToPkcs8(pem: string): Uint8Array {
  const normalized = pem.replace(/\\n/g, "\n");
  const der = decodeBase64(normalized.replace(PEM_ARMOR, "").replace(/\s+/g, ""));
  return normalized.includes(PKCS1_HEADER) ? wrapPkcs1(der) : der;
}

function wrapPkcs1(der: Uint8Array): Uint8Array {
  return encodeTLV(
    SEQUENCE,
    concat(
      PKCS8_VERSION,
      RSA_ALGORITHM_IDENTIFIER,
      encodeTLV(OCTET_STRING, der),
    ),
  );
}

function encodeTLV(tag: number, value: Uint8Array): Uint8Array {
  const length = encodeLength(value.length);
  const out = new Uint8Array(1 + length.length + value.length);
  out[0] = tag;
  out.set(length, 1);
  out.set(value, 1 + length.length);
  return out;
}

function encodeLength(length: number): Uint8Array {
  if (length < 0x80) {
    return new Uint8Array([length]);
  }
  const bytes: number[] = [];
  for (let rest = length; rest > 0; rest >>>= 8) {
    bytes.unshift(rest & 0xff);
  }
  return new Uint8Array([0x80 | bytes.length, ...bytes]);
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

function decodeBase64(value: string): Uint8Array {
  return Uint8Array.from(atob(value), (c) => c.charCodeAt(0));
}
