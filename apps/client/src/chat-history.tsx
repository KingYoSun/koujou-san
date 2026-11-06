import { Disclosure } from "./disclosure";
import type { ChatMessage } from "./chat-api";
import type { Saved } from "./client-selection-host";
import type { VersionRef } from "./model-connections";

export const chatHistoryLimit = 20;
export function ChatHistory({
  messages,
  chosen,
  onChange,
  disabled,
}: {
  messages: Saved<ChatMessage>[];
  chosen: VersionRef[];
  onChange: (value: VersionRef[]) => void;
  disabled: boolean;
}) {
  return (
    <Disclosure title={`履歴 ${chosen.length}件 · 直近${chatHistoryLimit}件`}>
      {() => (
        <fieldset disabled={disabled} className="chat-history-selection">
          <legend>次の送信に含める発言</legend>
          {!messages.length && <p>この相談に含める履歴はありません。</p>}
          {messages.map((message) => (
            <label key={message.id}>
              <input
                type="checkbox"
                checked={chosen.some((r) => r.id === message.id)}
                onChange={(e) =>
                  onChange(
                    messages
                      .filter((r) =>
                        r.id === message.id
                          ? e.target.checked
                          : chosen.some((selected) => selected.id === r.id),
                      )
                      .map(({ id, version }) => ({ id, version })),
                  )
                }
              />
              {message.value.role === "assistant" ? "AI" : "発言"}：
              {message.value.text.slice(0, 100)}
            </label>
          ))}
        </fieldset>
      )}
    </Disclosure>
  );
}
