from fastapi import FastAPI
from sqlalchemy import create_engine
from models.database import Base
from api import devices, events

# SQLite Local Database Setup for E2E Testing
engine = create_engine("sqlite:///./test-server.db", connect_args={"check_same_thread": False})
Base.metadata.create_all(bind=engine)

app = FastAPI(
    title="User Memory Ingest API (Local Test Mode)",
    version="1.0.0",
    description="The core ingestion engine running on SQLite purely for Windows agent testing"
)

# Share the engine with the routers safely using app state
app.state.engine = engine

app.include_router(devices.router)
app.include_router(events.router)

@app.get("/health", tags=["System"])
async def health_check():
    return {
        "status": "ok",
        "db": "sqlite_local",
        "pipeline": "disabled_for_test"
    }

if __name__ == "__main__":
    import uvicorn
    uvicorn.run("main:app", host="0.0.0.0", port=8443, reload=True)
