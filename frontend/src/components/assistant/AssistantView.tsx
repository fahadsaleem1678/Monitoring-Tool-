import { FormEvent, useMemo, useState } from "react";
import { askClusterAssistant, type ChatResponse } from "../../api/chat";

type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
  response?: ChatResponse;
  state?: "error";
};

type AssistantViewProps = {
  token: string;
};

const starterPrompts = [
  "Are my pods healthy?",
  "Any crash loops?",
  "Which pods are restarting?",
  "Are nodes ready?"
];

export function AssistantView({ token }: AssistantViewProps) {
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "welcome",
      role: "assistant",
      text: "Ask me about pod health, crash loops, restarts, image pulls, pending pods, nodes, or scrape targets."
    }
  ]);
  const [draft, setDraft] = useState("");
  const [loading, setLoading] = useState(false);

  const latestSuggestions = useMemo(() => {
    const lastAssistant = [...messages].reverse().find((message) => message.role === "assistant" && message.response);
    return lastAssistant?.role === "assistant" && lastAssistant.response?.suggestions.length
      ? lastAssistant.response.suggestions
      : starterPrompts;
  }, [messages]);

  async function submitMessage(message: string) {
    const trimmed = message.trim();
    if (!trimmed || loading) {
      return;
    }

    const userMessage: ChatMessage = { id: crypto.randomUUID(), role: "user", text: trimmed };
    setMessages((current) => [...current, userMessage]);
    setDraft("");
    setLoading(true);

    try {
      const response = await askClusterAssistant(token, trimmed);
      setMessages((current) => [
        ...current,
        {
          id: crypto.randomUUID(),
          role: "assistant",
          text: response.answer,
          response
        }
      ]);
    } catch (error) {
      setMessages((current) => [
        ...current,
        {
          id: crypto.randomUUID(),
          role: "assistant",
          text: error instanceof Error ? error.message : "Assistant request failed",
          state: "error"
        }
      ]);
    } finally {
      setLoading(false);
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void submitMessage(draft);
  }

  return (
    <section className="assistant-layout">
      <header className="assistant-header">
        <div>
          <h2>Cluster Assistant</h2>
          <p>Plain-language answers from approved read-only cluster checks.</p>
        </div>
        <span>Deterministic v1</span>
      </header>

      <section className="assistant-transcript" aria-live="polite">
        {messages.map((message) => (
          <article key={message.id} className={`chat-bubble ${message.role} ${message.state ?? ""}`}>
            <strong>{message.role === "user" ? "You" : "Assistant"}</strong>
            <p>{message.text}</p>
            {message.role === "assistant" && message.response && <AssistantDetails response={message.response} />}
          </article>
        ))}
        {loading && (
          <article className="chat-bubble assistant loading">
            <strong>Assistant</strong>
            <p>Checking the cluster...</p>
          </article>
        )}
      </section>

      <section className="suggested-prompts" aria-label="Suggested questions">
        {latestSuggestions.map((prompt) => (
          <button key={prompt} type="button" onClick={() => void submitMessage(prompt)} disabled={loading}>
            {prompt}
          </button>
        ))}
      </section>

      <form className="assistant-composer" onSubmit={handleSubmit}>
        <textarea
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder="Ask about pods, nodes, restarts, image pulls, or scrape targets"
          maxLength={500}
          disabled={loading}
        />
        <button type="submit" disabled={loading || draft.trim() === ""}>
          Send
        </button>
      </form>
    </section>
  );
}

function AssistantDetails({ response }: { response: ChatResponse }) {
  return (
    <details className="assistant-details">
      <summary>Checked {labelIntent(response.intent)}</summary>
      {response.facts.length > 0 && (
        <div className="assistant-facts">
          {response.facts.map((fact) => (
            <span key={`${fact.label}-${fact.value}`} className={fact.severity}>
              {fact.label}: {fact.value}
            </span>
          ))}
        </div>
      )}
      {response.queries.length > 0 && (
        <pre>{response.queries.join("\n")}</pre>
      )}
    </details>
  );
}

function labelIntent(intent: string) {
  if (intent === "unsupported") {
    return "supported prompt examples";
  }
  return intent.replaceAll("_", " ");
}
