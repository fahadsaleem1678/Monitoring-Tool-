# Deterministic Cluster Chatbot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an authenticated, read-only Assistant tab that answers common Kubernetes cluster health questions using deterministic backend intents and approved Prometheus queries.

**Architecture:** The Go backend owns intent matching, approved query selection, Prometheus execution, and plain-English answer formatting. The React frontend owns the chat transcript, suggested prompts, loading/error states, and display of query details. The v1 contract leaves room for future LLM intent classification without changing the UI API.

**Tech Stack:** Go `net/http` backend, existing Prometheus client, React + TypeScript + Vite frontend, existing CSS system.

## Global Constraints

- V1 is deterministic and maps supported question patterns to approved backend checks.
- The chatbot must not run arbitrary shell commands.
- The chatbot must not run arbitrary user-provided PromQL.
- The chatbot must not mutate Kubernetes resources.
- The endpoint is authenticated with the existing JWT middleware.
- The existing Prometheus client is the only V1 data source.
- Frontend styling matches the current compact dark operational dashboard.

---

## File Structure

- Create `backend/internal/chat/service.go`: deterministic intent catalog, matcher, Prometheus query execution, answer formatting, request/response types.
- Create `backend/internal/chat/service_test.go`: unit tests for matching, unsupported fallback, and formatter behavior.
- Create `backend/internal/httpapi/chat.go`: HTTP handler for `POST /api/v1/chat/query`.
- Create `backend/internal/httpapi/chat_test.go`: handler validation tests with a fake chat service.
- Modify `backend/cmd/server/main.go`: instantiate chat service and register the authenticated chat route.
- Create `frontend/src/api/chat.ts`: typed frontend API wrapper.
- Create `frontend/src/components/assistant/AssistantView.tsx`: chat UI and state handling.
- Modify `frontend/src/App.tsx`: add `Assistant` tab and render the new view.
- Modify `frontend/src/styles.css`: add Assistant layout styles consistent with the existing dashboard.

---

### Task 1: Backend Chat Service

**Files:**
- Create: `backend/internal/chat/service.go`
- Test: `backend/internal/chat/service_test.go`

**Interfaces:**
- Consumes: `PrometheusQuerier.InstantQuery(ctx context.Context, query string) (json.RawMessage, error)`
- Produces: `func NewService(prometheus PrometheusQuerier, namespace string) *Service`
- Produces: `func (s *Service) Ask(ctx context.Context, message string) (Response, error)`
- Produces: `type Response struct { Answer string; Intent string; Confidence string; Facts []Fact; Queries []string; Suggestions []string }`

- [ ] **Step 1: Write the failing service tests**

Create `backend/internal/chat/service_test.go` with these test cases:

```go
func TestServiceAskMatchesCrashLoops(t *testing.T)
func TestServiceAskReturnsUnsupportedForUnknownQuestion(t *testing.T)
func TestServiceAskFormatsHealthyZeroValue(t *testing.T)
func TestServiceAskRejectsEmptyAndLongMessages(t *testing.T)
```

The fake querier should return Prometheus vector data:

```go
type fakeQuerier struct {
	results map[string]json.RawMessage
}

func (f fakeQuerier) InstantQuery(_ context.Context, query string) (json.RawMessage, error) {
	result, ok := f.results[query]
	if !ok {
		return nil, fmt.Errorf("unexpected query %s", query)
	}
	return result, nil
}

func vector(value string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"resultType":"vector","result":[{"metric":{},"value":[123,%q]}]}`, value))
}
```

- [ ] **Step 2: Run service tests and confirm they fail**

Run: `docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./internal/chat`

Expected: FAIL because `backend/internal/chat` does not exist yet.

- [ ] **Step 3: Implement the chat service**

Create `backend/internal/chat/service.go` with:

```go
const (
	IntentUnsupported = "unsupported"
	ConfidenceHigh   = "high"
	ConfidenceLow    = "low"
	defaultNamespace = "monitoring-tool"
	maxMessageLength = 500
)
```

Define approved intents for `pod_health`, `pod_crashloops`, `pod_restarts`, `pod_image_pull_errors`, `pod_pending`, `node_ready`, `pod_running_count`, and `scrape_targets`. Each intent uses fixed keyword matching and fixed PromQL strings. Namespace-scoped checks interpolate only the configured namespace value.

- [ ] **Step 4: Run service tests and confirm they pass**

Run: `docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./internal/chat`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```powershell
git add backend/internal/chat/service.go backend/internal/chat/service_test.go
git commit -m "feat: add deterministic chat service"
```

---

### Task 2: Backend HTTP Endpoint

**Files:**
- Create: `backend/internal/httpapi/chat.go`
- Test: `backend/internal/httpapi/chat_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `Ask(ctx context.Context, message string) (chat.Response, error)`
- Produces: `POST /api/v1/chat/query`

- [ ] **Step 1: Write the failing handler tests**

Create `backend/internal/httpapi/chat_test.go` with these tests:

```go
func TestChatHandlerQueryReturnsAnswer(t *testing.T)
func TestChatHandlerQueryRejectsEmptyMessage(t *testing.T)
func TestChatHandlerQueryRejectsInvalidJSON(t *testing.T)
```

The success test posts `{"message":"any crash loops?"}` and expects HTTP 200 with `intent` set to `pod_crashloops`.

- [ ] **Step 2: Run handler tests and confirm they fail**

Run: `docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./internal/httpapi`

Expected: FAIL because `NewChatHandler` does not exist yet.

- [ ] **Step 3: Implement handler and route**

Create `backend/internal/httpapi/chat.go` with `NewChatHandler(service chatAsker) *ChatHandler` and `func (h *ChatHandler) Query(w http.ResponseWriter, r *http.Request)`.

Modify `backend/cmd/server/main.go`:

```go
chatService := chat.NewService(prom, "monitoring-tool")
chatHandler := httpapi.NewChatHandler(chatService)
mux.Handle("POST /api/v1/chat/query", authService.Middleware(http.HandlerFunc(chatHandler.Query)))
```

- [ ] **Step 4: Run backend tests**

Run: `docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```powershell
git add backend/internal/httpapi/chat.go backend/internal/httpapi/chat_test.go backend/cmd/server/main.go
git commit -m "feat: expose cluster assistant API"
```

---

### Task 3: Frontend Assistant View

**Files:**
- Create: `frontend/src/api/chat.ts`
- Create: `frontend/src/components/assistant/AssistantView.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/styles.css`

**Interfaces:**
- Consumes: `POST /api/v1/chat/query`
- Produces: `AssistantView({ token }: { token: string })`

- [ ] **Step 1: Add API wrapper**

Create `frontend/src/api/chat.ts` with:

```ts
export async function askClusterAssistant(token: string, message: string): Promise<ChatResponse>
```

- [ ] **Step 2: Add Assistant component**

Create `frontend/src/components/assistant/AssistantView.tsx` with a local message list, suggested prompt buttons, submit handler, loading state, error transcript item, and details block for facts and PromQL.

Seed suggestions:

```ts
["Are my pods healthy?", "Any crash loops?", "Which pods are restarting?", "Are nodes ready?"]
```

- [ ] **Step 3: Wire navigation**

Modify `frontend/src/App.tsx` so `activeView` includes `"assistant"`, add an `Assistant` tab, import `AssistantView`, and render it with `token={session.token}`.

- [ ] **Step 4: Add styles**

Modify `frontend/src/styles.css` with `.assistant-layout`, `.assistant-transcript`, `.chat-bubble`, `.suggested-prompts`, `.assistant-composer`, and `.assistant-details`. Keep 8px borders, existing colors, and responsive behavior.

- [ ] **Step 5: Run frontend build**

Run: `docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build`

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add frontend/src/api/chat.ts frontend/src/components/assistant/AssistantView.tsx frontend/src/App.tsx frontend/src/styles.css
git commit -m "feat: add cluster assistant view"
```

---

### Task 4: Final Verification

**Files:**
- Modify if needed: files touched by earlier tasks only.

**Interfaces:**
- Consumes: complete backend and frontend implementation.
- Produces: verified deterministic chatbot feature.

- [ ] **Step 1: Run full backend tests**

Run: `docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...`

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run: `docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build`

Expected: PASS.

- [ ] **Step 3: Check repository status**

Run: `git status --short`

Expected: clean after commits.

- [ ] **Step 4: Push commits**

Run: `git push origin main`

Expected: feature commits pushed to GitHub.
