package alerting

import (
	"strings"
	"testing"

	"monitoring-tool/backend/internal/store"
)

func TestFiringIncidentSummaryForRestartsIncludesNamespaceCommands(t *testing.T) {
	value := 3.0
	rule := store.AlertRule{
		Name:       "Pod restarts",
		PromQL:     `sum(increase(kube_pod_container_status_restarts_total{namespace="monitoring"}[15m]))`,
		Operator:   ">",
		Threshold:  0,
		ForSeconds: 60,
	}

	summary := firingIncidentSummary(rule, &value)

	for _, want := range []string{
		"AI Incident Summary",
		"One or more containers restarted recently",
		"CrashLoopBackOff",
		"kubectl get pods -n monitoring",
		"kubectl describe pod <pod> -n monitoring",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestFiringIncidentSummaryForTargetDownIncludesJob(t *testing.T) {
	value := 1.0
	rule := store.AlertRule{
		Name:       "Node exporter down",
		PromQL:     `sum(up{job="node-exporter"} == 0)`,
		Operator:   ">",
		Threshold:  0,
		ForSeconds: 30,
	}

	summary := firingIncidentSummary(rule, &value)

	for _, want := range []string{
		"A monitored target is missing or down",
		"job=node-exporter",
		`Check Prometheus target job "node-exporter"`,
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestFiringIncidentSummaryForKubeStateMetricsRegexUsesSpecificCommands(t *testing.T) {
	value := 1.0
	rule := store.AlertRule{
		Name:       "Kube State Metrics Missing Test",
		PromQL:     `absent(up{job=~".*kube-state-metrics.*"})`,
		Operator:   ">",
		Threshold:  0,
		ForSeconds: 15,
	}

	summary := firingIncidentSummary(rule, &value)

	for _, want := range []string{
		"job=kube-state-metrics",
		`Check Prometheus target job "kube-state-metrics"`,
		"kubectl get pods -n monitoring -l app.kubernetes.io/name=kube-state-metrics",
		"kubectl describe deployment prometheus-stack-kube-state-metrics -n monitoring",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestLabelValueIgnoresTemplateVariables(t *testing.T) {
	got := labelValue(`container_memory_working_set_bytes{namespace="$namespace"}`, "namespace")
	if got != "" {
		t.Fatalf("labelValue returned %q, want empty for template variable", got)
	}
}
