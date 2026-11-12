import { describe, expect, it } from "vitest";
import { buildAutomaticTags, expandObservationScopes, expandTagPlaceholders, getProjectName, taxonomyTags } from "../extensions/retain/tags.js";

describe("retain tags and scopes", () => {
  const config = { constantTags: ["harness:pi"], projectName: "stable-project", observationScopes: [["{project}"], ["{cwd}"]] } as any;
  const ctx = { cwd: "/tmp/somewhere/project", sessionId: "s1", parentId: "p1" };

  it("expands project independent of cwd", () => {
    expect(getProjectName(config, ctx.cwd)).toBe("stable-project");
    expect(expandTagPlaceholders(["{project}", "user:me"], config, ctx)).toEqual(["project:stable-project", "user:me"]);
  });

  it("builds automatic retain tags", () => {
    expect(buildAutomaticTags(config, ctx, "auto")).toEqual(expect.arrayContaining([
      "harness:pi",
      "session:s1",
      "parent:p1",
      "project:stable-project",
      "store_method:auto",
      "plane:experience",
      "kind:transcript",
      "tier:claimed",
      "source:pi",
    ]));
  });

  it("tags explicit saves as claimed lessons", () => {
    expect(buildAutomaticTags(config, ctx, "tool")).toEqual(expect.arrayContaining(["kind:lesson", "tier:claimed"]));
  });

  it("takes the source from constantTags when one is set (Hydra workers)", () => {
    const worker = { ...config, constantTags: ["source:hydra-worker"] };
    const tags = taxonomyTags(worker, "transcript");
    expect(tags).toContain("source:hydra-worker");
    expect(tags).not.toContain("source:pi");
    expect(buildAutomaticTags(worker, ctx, "auto").filter((t) => t.startsWith("source:"))).toEqual(["source:hydra-worker"]);
  });

  it("expands observation scopes at queue time", () => {
    const scopes = expandObservationScopes(config, ctx) as string[][];
    expect(scopes[0]).toEqual(["project:stable-project"]);
    expect(scopes[1][0]).toMatch(/cwd:.*\/tmp\/somewhere\/project$/);
  });
});
