package alerting

import (
	"fmt"
	"regexp"
	"strings"

	"monitoring-tool/backend/internal/store"
)

type incidentContext struct {
	Kind        string
	Impact      string
	LikelyCause string
	Commands    []string
}

func firingIncidentSummary(rule store.AlertRule, value *float64) string {
	context := inferIncidentContext(rule)
	return fmt.Sprintf("*AI Incident Summary*\nImpact: %s\nLikely cause: %s\nObserved: %s %s %.4g for %ds\nSuggested checks:\n%s",
		context.Impact,
		context.LikelyCause,
		formatValue(value),
		rule.Operator,
		rule.Threshold,
		rule.ForSeconds,
		formatCommands(context.Commands),
	)
}

func resolvedIncidentSummary(rule store.AlertRule, value *float64) string {
	context := inferIncidentContext(rule)
	return fmt.Sprintf("*AI Incident Summary*\nStatus: recovery detected for %s\nCurrent value: %s\nSuggested follow-up:\n%s",
		context.Kind,
		formatValue(value),
		formatCommands(context.Commands),
	)
}

func inferIncidentContext(rule store.AlertRule) incidentContext {
	query := strings.ToLower(rule.PromQL)
	namespace := labelValue(rule.PromQL, "namespace")
	pod := labelValue(rule.PromQL, "pod")
	job := labelHint(rule.PromQL, "job")

	if strings.Contains(query, "container_memory") || strings.Contains(query, "memory_working_set") {
		return incidentContext{
			Kind:        "memory pressure",
			Impact:      scopedImpact("Memory usage is above the alert threshold", namespace, pod, job),
			LikelyCause: "A workload may be leaking memory, caching aggressively, or running with limits that are too low.",
			Commands:    workloadCommands(namespace, pod),
		}
	}
	if strings.Contains(query, "cpu") || strings.Contains(query, "container_cpu") || strings.Contains(query, "node_cpu") {
		return incidentContext{
			Kind:        "CPU pressure",
			Impact:      scopedImpact("CPU usage is above the alert threshold", namespace, pod, job),
			LikelyCause: "A workload may be saturated, receiving unusual traffic, or stuck in a busy loop.",
			Commands:    workloadCommands(namespace, pod),
		}
	}
	if strings.Contains(query, "restart") || strings.Contains(query, "kube_pod_container_status_restarts_total") {
		return incidentContext{
			Kind:        "pod restart activity",
			Impact:      scopedImpact("One or more containers restarted recently", namespace, pod, job),
			LikelyCause: "CrashLoopBackOff, failing readiness/liveness probes, OOM kills, or a bad rollout.",
			Commands:    workloadCommands(namespace, pod),
		}
	}
	if strings.Contains(query, "absent") || strings.Contains(query, "up == 0") || strings.Contains(query, "up{") {
		return incidentContext{
			Kind:        "scrape target availability",
			Impact:      scopedImpact("A monitored target is missing or down", namespace, pod, job),
			LikelyCause: "The pod/service may be down, the endpoint changed, or Prometheus cannot scrape the target.",
			Commands:    targetCommands(namespace, pod, job),
		}
	}
	if strings.Contains(query, "kube_deployment") {
		return incidentContext{
			Kind:        "deployment health",
			Impact:      scopedImpact("A deployment is not matching its desired healthy state", namespace, pod, job),
			LikelyCause: "Pods may be pending, unavailable, crash-looping, or blocked by scheduling constraints.",
			Commands:    deploymentCommands(namespace),
		}
	}

	return incidentContext{
		Kind:        "metric threshold",
		Impact:      scopedImpact("A metric crossed its configured alert threshold", namespace, pod, job),
		LikelyCause: "The related workload or scrape target changed behavior and needs inspection.",
		Commands:    workloadCommands(namespace, pod),
	}
}

func scopedImpact(base, namespace, pod, job string) string {
	parts := []string{}
	if namespace != "" {
		parts = append(parts, "namespace="+namespace)
	}
	if pod != "" {
		parts = append(parts, "pod="+pod)
	}
	if job != "" {
		parts = append(parts, "job="+job)
	}
	if len(parts) == 0 {
		return base + "."
	}
	return base + " for " + strings.Join(parts, ", ") + "."
}

func workloadCommands(namespace, pod string) []string {
	ns := namespace
	if ns == "" {
		return []string{
			"kubectl get pods -A",
			"kubectl get events -A --sort-by=.lastTimestamp",
			"Open the related dashboard panel and narrow by namespace/pod labels.",
		}
	}
	targetPod := pod
	if targetPod == "" {
		targetPod = "<pod>"
	}
	return []string{
		fmt.Sprintf("kubectl get pods -n %s", ns),
		fmt.Sprintf("kubectl describe pod %s -n %s", targetPod, ns),
		fmt.Sprintf("kubectl logs %s -n %s --tail=100", targetPod, ns),
	}
}

func targetCommands(namespace, pod, job string) []string {
	if strings.Contains(job, "kube-state-metrics") {
		ns := namespace
		if ns == "" {
			ns = "monitoring"
		}
		return []string{
			fmt.Sprintf("Check Prometheus target job %q in Status > Targets", job),
			fmt.Sprintf("kubectl get pods -n %s -l app.kubernetes.io/name=kube-state-metrics", ns),
			fmt.Sprintf("kubectl get svc,endpoints -n %s | grep kube-state-metrics", ns),
			fmt.Sprintf("kubectl describe deployment prometheus-stack-kube-state-metrics -n %s", ns),
		}
	}
	if strings.Contains(job, "node-exporter") {
		ns := namespace
		if ns == "" {
			ns = "monitoring"
		}
		return []string{
			fmt.Sprintf("Check Prometheus target job %q in Status > Targets", job),
			fmt.Sprintf("kubectl get daemonset -n %s | grep node-exporter", ns),
			fmt.Sprintf("kubectl get pods -n %s -l app.kubernetes.io/name=prometheus-node-exporter", ns),
			"kubectl get nodes -o wide",
		}
	}
	if strings.Contains(job, "prometheus") {
		ns := namespace
		if ns == "" {
			ns = "monitoring"
		}
		return []string{
			fmt.Sprintf("Check Prometheus target job %q in Status > Targets", job),
			fmt.Sprintf("kubectl get pods -n %s | grep prometheus", ns),
			fmt.Sprintf("kubectl get svc,endpoints -n %s | grep prometheus", ns),
			fmt.Sprintf("kubectl describe pod <prometheus-pod> -n %s", ns),
		}
	}

	commands := workloadCommands(namespace, pod)
	if job != "" {
		commands = append([]string{fmt.Sprintf("Check Prometheus target job %q in Status > Targets", job)}, commands...)
	}
	return commands
}

func deploymentCommands(namespace string) []string {
	ns := namespace
	if ns == "" {
		ns = "<namespace>"
	}
	return []string{
		fmt.Sprintf("kubectl get deployments -n %s", ns),
		fmt.Sprintf("kubectl get pods -n %s", ns),
		fmt.Sprintf("kubectl describe deployment <deployment> -n %s", ns),
	}
}

func formatCommands(commands []string) string {
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, "- `"+command+"`")
	}
	return strings.Join(lines, "\n")
}

func labelValue(query, label string) string {
	value := rawLabelValue(query, label)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "$") || strings.Contains(value, ".") || strings.Contains(value, "*") {
		return ""
	}
	return value
}

func labelHint(query, label string) string {
	value := rawLabelValue(query, label)
	value = strings.Trim(value, ".*^$")
	value = strings.ReplaceAll(value, ".*", "")
	value = strings.ReplaceAll(value, "\\", "")
	return strings.TrimSpace(value)
}

func rawLabelValue(query, label string) string {
	pattern := regexp.MustCompile(label + `\s*(=|!=|=~|!~)\s*"([^"]+)"`)
	match := pattern.FindStringSubmatch(query)
	if len(match) < 3 {
		return ""
	}
	return match[2]
}
