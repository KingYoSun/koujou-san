import { useState, type ReactNode } from "react";

export function Disclosure({
  title,
  children,
  defaultOpen = false,
}: {
  title: string;
  children: () => ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <details
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>{title}</summary>
      {open && children()}
    </details>
  );
}
