export function StatusBadge({
  label,
  value,
  tone = "neutral",
}: {
  label: string;
  value: string;
  tone?: "neutral" | "positive" | "warning" | "negative";
}) {
  const mark = { neutral: "○", positive: "✓", warning: "△", negative: "!" }[
    tone
  ];
  return (
    <span className={`status-badge status-${tone}`}>
      <span aria-hidden="true">{mark}</span> {label}：{value}
    </span>
  );
}
