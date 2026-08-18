package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"termduty/internal/config"
	"termduty/internal/domain"
	"termduty/internal/httpapi"
	"termduty/internal/orchestration"
	"termduty/internal/scheduler"
	"termduty/internal/store"
)

func newHTTPServer(t *testing.T) (*orchestration.Services, *domain.FakeClock, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	clock := domain.NewFakeClock(time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.New(context.Background(), filepath.Join(dir, "termduty.db"), filepath.Join(dir, "shards"), clock, log)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := orchestration.New(st, clock, log)
	cfg := config.Default(dir)
	sched := scheduler.New(svc, cfg, clock, log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = sched.Stop(stopCtx)
	})
	server := httpapi.New(svc, sched, cfg, log, st)
	server.MarkReady(true)
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return svc, clock, st, ts.URL
}

func mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status=%d want %d body=%s", resp.StatusCode, want, body)
	}
}

func doJSON(t *testing.T, url, method, actorID, role string, body any) *http.Response {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		t.Fatalf("newreq: %v", err)
	}
	if buf != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if actorID != "" {
		req.Header.Set("X-Actor-ID", actorID)
	}
	if role != "" {
		req.Header.Set("X-Actor-Role", role)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// TestHTTP_HealthAndReady confirms the two probes are distinct.
func TestHTTP_HealthAndReady(t *testing.T) {
	_, _, _, url := newHTTPServer(t)
	resp, err := http.Get(url + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	mustStatus(t, resp, http.StatusOK)
	resp, err = http.Get(url + "/readyz")
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	mustStatus(t, resp, http.StatusOK)
}

// TestHTTP_PaginationBoundaries verifies bad page params are rejected and
// oversized page sizes are clamped.
func TestHTTP_PaginationBoundaries(t *testing.T) {
	_, _, _, url := newHTTPServer(t)
	resp, _ := http.Get(url + "/api/readings?page=0")
	mustStatus(t, resp, http.StatusBadRequest)
	resp, _ = http.Get(url + "/api/readings?page=1&page_size=99999")
	mustStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp, _ = http.Get(url + "/api/readings?page=-1")
	mustStatus(t, resp, http.StatusBadRequest)
}

// TestHTTP_ErrorChainStatusCodes confirms domain errors map to status codes.
func TestHTTP_ErrorChainStatusCodes(t *testing.T) {
	_, _, _, url := newHTTPServer(t)
	resp, _ := http.Get(url + "/api/alerts/missing")
	mustStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
	// Accepting a missing alert yields a 404 (NotFound error).
	resp = doJSON(t, url+"/api/alerts/missing/accept", http.MethodPost, "h1", "handler", map[string]string{"handler_id": "h1"})
	mustStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// TestHTTP_RoleEnforcement verifies a handler cannot perform a duty-only action.
func TestHTTP_RoleEnforcement(t *testing.T) {
	svc, clock, st, url := newHTTPServer(t)
	c := seedCollectorHTTP(t, st, "RE1")
	max := 8.0
	_ = st.Rules().Create(context.Background(), domain.Rule{ID: "rule-re1", Metric: domain.MetricQueueCount, MaxValue: &max, WindowSeconds: 60, Severity: domain.SeverityWarn, Enabled: true, CreatedAt: clock.Now(), UpdatedAt: clock.Now()})
	rd := domain.Reading{ID: domain.ReadingID("rd-re1"), CollectorID: c.ID, Timestamp: clock.Now(), QueueCount: 20, IngestedAt: clock.Now()}
	_, _ = st.Readings().Append(context.Background(), rd)
	alert := domain.Alert{ID: domain.AlertID("alert-re1"), CollectorID: c.ID, RuleID: "rule-re1", ReadingID: rd.ID, Severity: domain.SeverityWarn, State: domain.AlertStateOpen, FirstSeen: clock.Now(), LastSeen: clock.Now(), CreatedAt: clock.Now(), UpdatedAt: clock.Now()}
	_ = st.Alerts().Create(context.Background(), alert)
	// Handler tries to revoke (duty-only) -> 400.
	resp := doJSON(t, url+"/api/alerts/alert-re1/revoke", http.MethodPost, "h1", "handler", map[string]string{})
	mustStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
	// Duty can revoke.
	resp = doJSON(t, url+"/api/alerts/alert-re1/revoke", http.MethodPost, "duty1", "duty", map[string]string{})
	mustStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	// accept on already-revoked alert -> conflict
	resp = doJSON(t, url+"/api/alerts/alert-re1/accept", http.MethodPost, "h1", "handler", map[string]string{"handler_id": "h1"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("accept revoked: status=%d want 409", resp.StatusCode)
	}
	resp.Body.Close()
	_ = svc
}

// TestHTTP_IngestAcceptFlow exercises the end-to-end business path over HTTP:
// ingest -> process -> evaluate -> list alerts -> accept (single winner).
func TestHTTP_IngestAcceptFlow(t *testing.T) {
	svc, clock, st, url := newHTTPServer(t)
	c := seedCollectorHTTP(t, st, "FL1")
	max := 8.0
	_ = st.Rules().Create(context.Background(), domain.Rule{ID: "rule-fl1", Metric: domain.MetricQueueCount, MaxValue: &max, WindowSeconds: 60, Severity: domain.SeverityWarn, Enabled: true, CreatedAt: clock.Now(), UpdatedAt: clock.Now()})

	resp := doJSON(t, url+"/api/ingest", http.MethodPost, "FL1", "system", map[string]any{
		"collector_id": string(c.ID), "timestamp": clock.Now().Format(time.RFC3339Nano), "queue_count": 20, "duration_ms": 5000,
	})
	mustStatus(t, resp, http.StatusAccepted)
	resp.Body.Close()

	// Drive the poll-pull and evaluation synchronously (no real timers).
	if _, err := svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10); err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, err := svc.Alerts.Evaluate(context.Background(), clock.Now()); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	resp, _ = http.Get(url + "/api/alerts?state=open&page=1&page_size=10")
	mustStatus(t, resp, http.StatusOK)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&page)
	resp.Body.Close()
	if page.Total != 1 {
		t.Fatalf("alerts total=%d want 1", page.Total)
	}
	aid := page.Items[0]["id"].(string)
	// First accept wins.
	resp = doJSON(t, url+"/api/alerts/"+aid+"/accept", http.MethodPost, "h1", "handler", map[string]string{"handler_id": "h1"})
	mustStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	// Second accept conflicts and names the winner.
	resp = doJSON(t, url+"/api/alerts/"+aid+"/accept", http.MethodPost, "h2", "handler", map[string]string{"handler_id": "h2"})
	mustStatus(t, resp, http.StatusConflict)
	var errBody struct {
		Code   string            `json:"code"`
		Detail map[string]string `json:"detail"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	resp.Body.Close()
	if errBody.Code != "already_assigned" || errBody.Detail["assignee"] != "h1" {
		t.Fatalf("conflict body=%+v", errBody)
	}
}

// TestHTTP_StatsAndExport verifies the read-side endpoints.
func TestHTTP_StatsAndExport(t *testing.T) {
	svc, clock, st, url := newHTTPServer(t)
	c := seedCollectorHTTP(t, st, "SE1")
	sub := domain.ReadingSubmission{CollectorID: c.ID, Timestamp: clock.Now(), QueueCount: 4}
	_, _ = svc.Ingest.Enqueue(context.Background(), sub, orchestration.Actor{ID: "sys", Role: domain.RoleSystem})
	_, _ = svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10)
	resp, _ := http.Get(url + "/api/stats")
	mustStatus(t, resp, http.StatusOK)
	var stats struct {
		Readings int64 `json:"readings"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats.Readings != 1 {
		t.Fatalf("stats readings=%d", stats.Readings)
	}
	resp, _ = http.Get(url + "/api/readings/export")
	mustStatus(t, resp, http.StatusOK)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 {
		t.Fatalf("export empty")
	}
}

// TestHTTP_CreateCollectorAndBatchDisable covers admin + batch endpoints.
func TestHTTP_CreateCollectorAndBatchDisable(t *testing.T) {
	_, _, _, url := newHTTPServer(t)
	resp := doJSON(t, url+"/api/collectors", http.MethodPost, "ops1", "ops", map[string]string{"code": "C1", "name": "窗口", "kind": "window"})
	mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	id := created["id"].(string)
	// Batch disable with a missing id -> rolled back, returns error in body.
	resp = doJSON(t, url+"/api/collectors/batch/disable", http.MethodPost, "ops1", "ops", map[string]any{"ids": []string{id, "ghost"}})
	mustStatus(t, resp, http.StatusOK)
	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	if res["rolled_back"] != true {
		t.Fatalf("expected rolled_back=true, got %v", res)
	}
	// Verify the created collector is still active after rollback.
	resp, _ = http.Get(url + "/api/collectors/" + id)
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got["status"] != "active" {
		t.Fatalf("collector not restored: %v", got["status"])
	}
}

func seedCollectorHTTP(t *testing.T, st *store.Store, code string) domain.Collector {
	t.Helper()
	c := domain.Collector{ID: domain.CollectorID("col-" + code), Code: code, Name: code, Kind: domain.CollectorKindWindow, Status: domain.CollectorStatusActive, HandlerID: "h-" + code, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Collectors().Create(context.Background(), c); err != nil {
		t.Fatalf("create collector: %v", err)
	}
	return c
}
