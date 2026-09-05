FROM python:3.12-slim
WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app

EXPOSE 8000
# Shell form so ${PORT} is honored when a platform injects it (Render,
# Railway, Cloud Run, etc. commonly require binding to $PORT); falls
# back to 8000 to match EXPOSE and every prior milestone's local usage.
CMD uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8000}
