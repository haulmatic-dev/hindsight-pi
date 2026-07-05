import { describe, expect, it } from "vitest";
import { coalesceByDocumentId, type QueueRecord } from "../extensions/queue.js";

const rec = (documentId: string | undefined, content: string, tags?: string[]): QueueRecord => ({
  sessionId: "s",
  bankId: "b",
  content,
  timestamp: "2026-07-05T00:00:00Z",
  document_id: documentId,
  tags,
});

describe("coalesceByDocumentId", () => {
  it("merges records that share a document_id into one, concatenating content", () => {
    const out = coalesceByDocumentId([rec("d1", "a"), rec("d1", "b"), rec("d2", "c")]);
    expect(out).toHaveLength(2);
    const d1 = out.find((r) => r.document_id === "d1");
    expect(d1?.content).toBe("a\n\nb");
  });

  it("guarantees unique document_ids in the result (no batch duplicates)", () => {
    const out = coalesceByDocumentId([rec("d1", "a"), rec("d1", "b"), rec("d1", "c")]);
    const ids = out.map((r) => r.document_id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(out).toHaveLength(1);
  });

  it("passes through records without a document_id unchanged", () => {
    const out = coalesceByDocumentId([rec(undefined, "x"), rec(undefined, "y")]);
    expect(out).toHaveLength(2);
  });

  it("unions tags across merged records", () => {
    const out = coalesceByDocumentId([rec("d1", "a", ["x"]), rec("d1", "b", ["y", "x"])]);
    expect(out[0].tags?.slice().sort()).toEqual(["x", "y"]);
  });

  it("does not mutate the input records", () => {
    const first = rec("d1", "a", ["x"]);
    coalesceByDocumentId([first, rec("d1", "b")]);
    expect(first.content).toBe("a");
    expect(first.tags).toEqual(["x"]);
  });
});
