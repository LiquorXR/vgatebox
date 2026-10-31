import type { ConsoleSummary } from "@/api/types";

/**
 * A saved target of zero must stay zero. Only an instance that has never saved
 * a target falls back to the suggested default (6).
 */
export function initialTarget(
  summary: Pick<ConsoleSummary, "target" | "target_configured" | "suggested_target">,
): number {
  if (summary.target_configured === false) return summary.suggested_target || 6;
  return summary.target;
}
