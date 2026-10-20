import { useId } from "react";

export function Field({
  label,
  value,
  onChange,
  rows,
  hint,
  error,
  required = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  rows?: number;
  hint?: string;
  error?: string;
  required?: boolean;
}) {
  const id = useId();
  const props = {
    id,
    value,
    required,
    "aria-invalid": !!error,
    "aria-describedby": hint || error ? id + "-help" : undefined,
    onChange: (
      event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
    ) => onChange(event.target.value),
  };
  return (
    <label className="field" htmlFor={id}>
      <span>
        {label}
        {required && "（必須）"}
      </span>
      {rows ? <textarea {...props} rows={rows} /> : <input {...props} />}
      <small id={id + "-help"}>
        {error ? <span role="alert">{error}</span> : hint}
      </small>
    </label>
  );
}

export function jsonError(value: string) {
  try {
    JSON.parse(value);
    return undefined;
  } catch {
    return "JSONの形式を確認してください。引用符・区切り・括弧を修正してください。";
  }
}
