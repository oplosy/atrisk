import type { ReactNode } from "react";

export type QualityState = "valid" | "degraded" | "blocked";

const qualityCopy: Record<QualityState, { label: string; icon: string }> = {
  valid: { label: "Valid", icon: "✓" },
  degraded: { label: "Degraded", icon: "!" },
  blocked: { label: "Blocked", icon: "×" },
};

interface QualityBadgeProps {
  state: QualityState;
  children?: ReactNode;
}

export function QualityBadge({ state, children }: QualityBadgeProps) {
  const copy = qualityCopy[state];
  return (
    <span className={`quality-badge quality-badge--${state}`}>
      <span aria-hidden="true" className="quality-badge__icon">
        {copy.icon}
      </span>
      <span>{children ?? copy.label}</span>
    </span>
  );
}
