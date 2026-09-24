import { afterEach, describe, expect, it, vi } from "vitest";
import { ensureBank, getBankInfo } from "../extensions/client.js";

// hindsight-api 0.10.x removed the bank-profile endpoints (GET /v1/default/banks/{id}
// returns 405). Existence checks must go through the list endpoint.

const config = { autoCreateBank: true, workspace: "/tmp/ws", baseUrl: "http://hindsight.test" };

const mockFetch = (impl: (url: string) => { status: number; body?: any }) => {
  vi.stubGlobal("fetch", vi.fn(async (url: any) => {
    const r = impl(String(url));
    return {
      ok: r.status >= 200 && r.status < 300,
      status: r.status,
      statusText: String(r.status),
      json: async () => r.body,
    } as Response;
  }));
};

afterEach(() => vi.unstubAllGlobals());

describe("getBankInfo (hindsight-api 0.10.x)", () => {
  it("finds a bank via the list endpoint", async () => {
    mockFetch(() => ({ status: 200, body: { banks: [{ bank_id: "coding-brain", name: "coding-brain" }] } }));
    const info = await getBankInfo("http://hindsight.test", undefined, "coding-brain");
    expect(info?.bank_id).toBe("coding-brain");
    expect(fetch).toHaveBeenCalledWith("http://hindsight.test/v1/default/banks", expect.anything());
  });

  it("returns null when the bank is absent", async () => {
    mockFetch(() => ({ status: 200, body: { banks: [{ bank_id: "other" }] } }));
    expect(await getBankInfo("http://hindsight.test", undefined, "coding-brain")).toBeNull();
  });
});

describe("ensureBank (hindsight-api 0.10.x)", () => {
  it("never calls the removed per-bank profile endpoint", async () => {
    mockFetch(() => ({ status: 200, body: { banks: [{ bank_id: "coding-brain" }] } }));
    const client = { getBankProfile: vi.fn(), createBank: vi.fn() };
    await ensureBank(client, "coding-brain", config);
    expect(client.getBankProfile).not.toHaveBeenCalled();
    expect(client.createBank).not.toHaveBeenCalled();
  });

  it("creates the bank when missing and autoCreateBank is set", async () => {
    mockFetch(() => ({ status: 200, body: { banks: [] } }));
    const client = { createBank: vi.fn(async () => ({})) };
    await ensureBank(client, "coding-brain", config);
    expect(client.createBank).toHaveBeenCalledWith("coding-brain", expect.objectContaining({ name: "coding-brain" }));
  });

  it("throws when the bank is missing and autoCreateBank is off", async () => {
    mockFetch(() => ({ status: 200, body: { banks: [] } }));
    await expect(ensureBank({}, "coding-brain", { ...config, autoCreateBank: false })).rejects.toThrow("not found");
  });
});
