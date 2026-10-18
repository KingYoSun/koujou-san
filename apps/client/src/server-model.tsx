export type ModelConnection = {
  id: string;
  name: string;
  revision: string;
  models: { requested: string; allowed_actual: string[] }[];
};
export type SemanticSettings = {
  connection: string;
  revision: string;
  model: string;
  allowed_models: string[];
  fallbacks: string[];
  instructions: string;
  limits: { input_bytes: number; output_tokens: number; seconds: number };
  reevaluate_evidence: boolean;
};

export function ServerModelFields({
  settings,
  connections,
  onChange,
}: {
  settings: SemanticSettings;
  connections: ModelConnection[];
  onChange: (patch: Partial<SemanticSettings>) => void;
}) {
  return (
    <>
      {" "}
      <label>
        モデル接続
        <select
          value={settings.connection}
          onChange={(e) => {
            const c = connections.find((c) => c.id === e.target.value);
            if (c)
              onChange({
                connection: c.id,
                revision: c.revision,
                model: c.models[0].requested,
                allowed_models: c.models[0].allowed_actual,
                fallbacks: [],
              });
          }}
        >
          <option value={settings.connection}>{settings.connection}</option>
          {connections
            .filter((c) => c.id !== settings.connection)
            .map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
        </select>
      </label>
      <label>
        要求モデル
        <select
          value={settings.model}
          onChange={(e) => {
            const m = connections
              .find((c) => c.id === settings.connection)
              ?.models.find((m) => m.requested === e.target.value);
            if (m)
              onChange({
                model: m.requested,
                allowed_models: m.allowed_actual,
                fallbacks: [],
              });
          }}
        >
          <option value={settings.model}>{settings.model}</option>
          {connections
            .find((c) => c.id === settings.connection)
            ?.models.filter((m) => m.requested !== settings.model)
            .map((m) => (
              <option key={m.requested} value={m.requested}>
                {m.requested}
              </option>
            ))}
        </select>
      </label>
      <label>
        評価への指示
        <textarea
          value={settings.instructions}
          onChange={(e) => onChange({ instructions: e.target.value })}
        />
      </label>
      {(
        [
          ["input_bytes", "入力上限（UTF-8 byte）"],
          ["output_tokens", "出力上限（token）"],
          ["seconds", "要求の時間上限（秒）"],
        ] as const
      ).map(([key, label]) => (
        <label key={key}>
          {label}
          <input
            type="number"
            min={1}
            value={settings.limits[key]}
            onChange={(e) =>
              onChange({
                limits: {
                  ...settings.limits,
                  [key]: Number(e.target.value),
                },
              })
            }
          />
        </label>
      ))}
    </>
  );
}
