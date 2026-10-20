import { useRef, type ReactNode } from "react";

type Item = { id: string; label: string; disabledReason?: string };

export function Tabs({
  id,
  label,
  items,
  value,
  onChange,
}: {
  id: string;
  label: string;
  items: Item[];
  value: string;
  onChange: (value: string) => void;
}) {
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  return (
    <div className="tabs" role="tablist" aria-label={label}>
      {items.map((item) => (
        <button
          key={item.id}
          ref={(node) => {
            if (node) buttons.current.set(item.id, node);
            else buttons.current.delete(item.id);
          }}
          type="button"
          role="tab"
          id={`${id}-tab-${item.id}`}
          aria-controls={`${id}-panel-${item.id}`}
          aria-selected={value === item.id}
          tabIndex={value === item.id ? 0 : -1}
          disabled={!!item.disabledReason}
          title={item.disabledReason}
          onClick={() => onChange(item.id)}
          onKeyDown={(event) => {
            const enabled = items.filter((v) => !v.disabledReason);
            const index = enabled.findIndex((v) => v.id === item.id);
            const next =
              event.key === "Home"
                ? 0
                : event.key === "End"
                  ? enabled.length - 1
                  : event.key === "ArrowRight"
                    ? (index + 1) % enabled.length
                    : event.key === "ArrowLeft"
                      ? (index + enabled.length - 1) % enabled.length
                      : undefined;
            if (next === undefined) return;
            event.preventDefault();
            onChange(enabled[next].id);
            buttons.current.get(enabled[next].id)?.focus();
          }}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}

export function TabPanel({
  id,
  value,
  active,
  children,
}: {
  id: string;
  value: string;
  active: string;
  children: ReactNode;
}) {
  return value === active ? (
    <section
      role="tabpanel"
      id={`${id}-panel-${value}`}
      aria-labelledby={`${id}-tab-${value}`}
      tabIndex={0}
    >
      {children}
    </section>
  ) : null;
}
