import { useId } from "react";
import type { JSONPath } from "jsonc-parser";
import { fieldSource } from "./workflow-json";

export function JSONField({
  label,
  path,
  text,
  fields,
  onChange,
  format = "text",
  options,
}: {
  label: string;
  path: JSONPath;
  text: string;
  fields: Record<string, string | null>;
  onChange: (path: string, raw: string | null) => void;
  format?: "text" | "json" | "number" | "boolean";
  options?: readonly { value: string; label: string }[];
}) {
  const id = useId(),
    key = JSON.stringify(path);
  let value = fields[key] ?? "",
    error = "";
  if (!(key in fields)) {
    try {
      value = fieldSource(text, path);
    } catch {
      /* Preserve the original invalid document. */
    }
  }
  if (format === "text") {
    try {
      value = JSON.parse(value) as string;
    } catch {
      value = "";
    }
  } else if (value) {
    try {
      JSON.parse(value);
    } catch {
      error = "JSONの形式を確認してください。入力は保持しています。";
    }
  }
  const props = {
    id,
    value,
    "aria-invalid": !!error,
    "aria-describedby": error ? id + "-error" : undefined,
    onChange: (
      e: React.ChangeEvent<
        HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement
      >,
    ) =>
      onChange(
        key,
        format === "text"
          ? JSON.stringify(e.target.value)
          : e.target.value || null,
      ),
  };
  if (format === "boolean")
    return (
      <div className="field-check">
        <input
          id={id}
          type="checkbox"
          checked={value === "true"}
          onChange={(e) => onChange(key, JSON.stringify(e.target.checked))}
        />
        <label htmlFor={id}>{label}</label>
      </div>
    );
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {options ? (
        <select {...props}>
          {options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      ) : format === "json" ? (
        <textarea {...props} rows={3} />
      ) : (
        <input {...props} type={format === "number" ? "number" : "text"} />
      )}
      {error && (
        <small role="alert" id={id + "-error"}>
          {error}
        </small>
      )}
    </div>
  );
}
