# Deterministic Cluster Chatbot Design

## Purpose

Add a read-only chatbot view that lets non-technical users ask common Kubernetes cluster health questions in plain language. The first version will be deterministic: it maps supported question patterns to approved backend checks, runs only predefined Prometheus queries, and returns concise human-readable answers.

This keeps the demo reliable while creating a clean future path to LLM-backed natural language.

## Scope

V1 supports common cluster status questions:

- Overall pod health.
- CrashLoopBackOff pods.
- Restarting pods.
- Image pull errors.
- Pending pods.
- Ready nodes.
- Running pod count.
- Unhealthy scrape targets.

Out of scope for V1:

- Arbitrary shell commands.
- Arbitrary user-provided PromQL from the chatbot.
- Any Kubernetes mutation such as delete, scale, patch, or rollout restart.
- LLM-based intent detection.
- Long conversational memory beyond the current frontend message list.

## Architecture

The Go backend owns chatbot intent detection and query execution.

Add a new authenticated endpoint:

```text
POST /api/v1/chat/query
```

The frontend sends a user message and receives:

- `answer`: plain-English summary.
- `intent`: matched deterministic intent.
- `confidence`: deterministic confidence label.
- `facts`: structured values used to create the answer.
- `queries`: approved PromQL queries that were executed.
- `suggestions`: example follow-up questions when unsupported or ambiguous.

The backend will implement a small `chat` package with:

- an intent matcher that normalizes the question and selects a known intent.
- a catalog of approved checks and PromQL queries.
- answer formatting that turns Prometheus vector results into plain language.

The existing Prometheus client remains the only data source for V1.

## Data Flow

1. User opens the new `Assistant` tab.
2. User asks a question, such as `any crash loops?`.
3. Frontend calls `POST /api/v1/chat/query` with the active auth token.
4. Backend normalizes the text and matches it to an approved intent.
5. Backend runs the predefined PromQL query or small query group for that intent.
6. Backend returns an answer and supporting facts.
7. Frontend appends the assistant response to the chat history.

Unsupported questions receive a helpful fallback answer with suggested prompts instead of attempting unsafe execution.

## Backend Contract

Request:

```json
{
  "message": "are there any crash loops?"
}
```

Successful response:

```json
{
  "answer": "There are 2 pods showing CrashLoopBackOff in monitoring-tool.",
  "intent": "pod_crashloops",
  "confidence": "high",
  "facts": [
    {
      "label": "CrashLoopBackOff pods",
      "value": "2",
      "severity": "warning"
    }
  ],
  "queries": [
    "sum(kube_pod_container_status_waiting_reason{namespace=\"monitoring-tool\",reason=\"CrashLoopBackOff\"})"
  ],
  "suggestions": [
    "Which pods are restarting?",
    "Any image pull errors?"
  ]
}
```

Unsupported response:

```json
{
  "answer": "I can only answer a focused set of read-only cluster health questions right now.",
  "intent": "unsupported",
  "confidence": "low",
  "facts": [],
  "queries": [],
  "suggestions": [
    "Are my pods healthy?",
    "Any crash loops?",
    "Are nodes ready?"
  ]
}
```

## Frontend Design

Add `Assistant` to the main tab navigation.

The view contains:

- A chat transcript with user and assistant bubbles.
- A compact composer with a multiline input and send button.
- Suggested prompt buttons for common questions.
- Loading, error, unsupported, and empty states.
- A small details area per assistant answer showing matched intent and checked PromQL.

The UI should match the existing operational style: dark background, compact panels, 8px radius, high scanability, no marketing-style hero area.

## Error Handling

Backend:

- Reject empty messages with HTTP 400.
- Limit message length to prevent accidental large requests.
- Return `unsupported` for questions outside the catalog.
- Return HTTP 502 if Prometheus cannot answer a matched check.
- Keep all checks read-only.

Frontend:

- Disable sending while a request is running.
- Keep the user's question visible even when the backend fails.
- Show a concise failure response in the transcript.
- Preserve suggested prompts so the user can recover quickly.

## Testing

Backend tests:

- Intent matching for supported phrasings.
- Unsupported question fallback.
- Response formatting for zero, warning, and error-like values.
- HTTP handler validation for empty and long messages.

Frontend validation:

- TypeScript build.
- Manual UI smoke test for supported question, unsupported question, loading state, and error display.

Existing validation commands:

```powershell
docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

## Future LLM Path

V2 can add LLM-backed intent detection without changing the frontend contract:

- LLM classifies the user message into one of the approved intents.
- Backend still executes only the approved query catalog.
- Unknown or risky requests still fall back to unsupported.
- Later, Kubernetes MCP read-only tools can enrich answers with pod names, namespaces, and recent logs.

This preserves safety while improving natural language coverage.
