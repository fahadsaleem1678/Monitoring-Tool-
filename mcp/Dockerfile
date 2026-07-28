FROM python:3.12-alpine

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
ARG MCP_APP
COPY ${MCP_APP} ./service
CMD ["uvicorn", "service.app:app", "--host", "0.0.0.0", "--port", "8091"]
