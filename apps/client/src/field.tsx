import { useId } from "react";

export function Field({
  label,
  value,
  onChange,
  rows,
  error,
  required = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  rows?: number;
  error?: string;
  required?: boolean;
}) {
  const id = useId();
  const props = {
    id,
    value,
    required,
    "aria-invalid": !!error,
    "aria-describedby": error ? id + "-help" : undefined,
    onChange: (
      event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
    ) => onChange(event.target.value),
  };
  return (
    <div className="field">
      <label htmlFor={id}>
        {label}
        {required && "（必須）"}
      </label>
      {rows ? <textarea {...props} rows={rows} /> : <input {...props} />}
      {error && (
        <small id={id + "-help"}>
          <span role="alert">{error}</span>
        </small>
      )}
    </div>
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
