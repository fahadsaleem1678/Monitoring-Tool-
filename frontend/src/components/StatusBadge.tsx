type StatusBadgeProps = {
  status: "loading" | "healthy" | "error";
};

const labelByStatus = {
  loading: "Checking",
  healthy: "Healthy",
  error: "Offline"
};

export function StatusBadge({ status }: StatusBadgeProps) {
  return <span className={`status-badge ${status}`}>{labelByStatus[status]}</span>;
}
