export type CostRef = { id: string; version: string };
export type CostCondition = {
  mode: string;
  kind: string;
  from: string;
  until: string;
  timezone: string;
  scopes: string[];
  workflow: string;
  model: string;
  currency: string;
  attribution: string;
  attribution_id: string;
  basis: CostRef[];
  limit: number;
  cursor?: string;
};
export type CostComparison = {
  name: string;
  left: CostCondition;
  right: CostCondition;
};
export type CostRow = {
  key: string;
  attribution_kind?: string;
  attribution_id?: string;
  model?: string;
  currency?: string;
  kind?: string;
  records?: number;
  amount?: string | null;
  state?: string;
  measurement?: string;
  measured_scope?: string;
  billing?: string;
  group?: string;
  refs?: CostRef[];
  inspection?: CostRef;
  source?: string;
  verdict?: string;
  quantities?: {
    category: string;
    unit: string;
    value: string | null;
    reported: number;
    estimated: number;
    missing: number;
    uncertain?: number;
    not_applicable: number;
  }[];
  value?: Record<string, unknown>;
};
export type CostSummary = {
  runs: number;
  completed: number;
  failed: number;
  unfinished: number;
  rework: number;
  execution_seconds: string | null;
  waiting_seconds: string | null;
  time_unavailable: number;
  run_metadata_unavailable: number;
  quality: {
    source: string;
    state: string;
    verdict: string;
    records: number;
  }[];
};
export type CostPage = {
  data: {
    items: CostRow[];
    as_of: string;
    through: number;
    timezone: string;
    kind: string;
    summary?: CostSummary;
  };
  meta: { next_cursor: string | null };
};
export const costLabels: Record<string, string> = {
  price: "料金版",
  evaluation: "費用評価",
  input: "入力",
  output: "出力",
  cache: "キャッシュ",
  cache_read: "キャッシュ読取り",
  cache_write: "キャッシュ作成",
  reasoning: "推論",
  usage: "利用量",
  reported: "報告額",
  estimated: "単価からの推定額",
  api_equivalent: "API料金換算額",
  allocated: "配賦額",
  run: "Run実行",
  operation: "選択・生成の操作",
  hopper: "Hopper評価",
  triage: "Triage評価",
  unattributed: "未帰属",
  rated: "算定済み",
  unrated: "未算定",
  pending: "再評価待ち",
  confirmed: "計測範囲確認済み",
  review_needed: "計測範囲の確認が必要",
  final: "終了時取得",
  partial: "一部計測",
  provisional: "途中の計測",
  unavailable: "未取得",
  not_applicable: "非該当",
  subscription: "定額契約",
  api: "API従量課金",
  local: "ローカル実行",
  unknown: "未確認",
  independent: "独立検品の保存時判定",
  "self-report": "CLIの自己申告",
  unverified: "検品未確認",
  unobserved: "未確認",
  valid: "保存時に有効",
  invalid: "無効",
};
export const costLabel = (value?: string) =>
  value ? (costLabels[value] ?? value) : "未取得";
export const amountLabel = (
  value: string | null | undefined,
  currency?: string,
) => (value == null ? "未算定" : `${value}${currency ? " " + currency : ""}`);
export function newCondition(): CostCondition {
  return {
    mode: "compare",
    kind: "api_equivalent",
    from: new Date().toISOString().slice(0, 10) + "T00:00:00Z",
    until:
      new Date(Date.now() + 86400000).toISOString().slice(0, 10) + "T00:00:00Z",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    scopes: [],
    workflow: "",
    model: "",
    currency: "",
    attribution: "",
    attribution_id: "",
    basis: [],
    limit: 50,
  };
}
export function openCostEditor(id: string) {
  const element = document.getElementById(id);
  if (!element) return;
  let parent: HTMLElement | null = element;
  while (parent) {
    if (parent instanceof HTMLDetailsElement) parent.open = true;
    parent = parent.parentElement;
  }
  element.scrollIntoView({ block: "start", behavior: "smooth" });
  element.focus({ preventScroll: true });
}

export function storedCondition(value: Partial<CostCondition>): CostCondition {
  return {
    ...newCondition(),
    ...value,
    scopes: value.scopes ?? [],
    basis: value.basis ?? [],
    cursor: undefined,
  };
}

export function distinctCostRefs(refs: CostRef[] = []): CostRef[] {
  return [
    ...new Map(refs.map((ref) => [ref.id + "@" + ref.version, ref])).values(),
  ];
}
