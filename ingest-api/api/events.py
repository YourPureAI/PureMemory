from fastapi import APIRouter, HTTPException, BackgroundTasks, Header, Request
from sqlalchemy.orm import Session
from typing import Optional
from models.events import BatchPayload
from models.database import RawEvent
import gzip
import json

router = APIRouter(prefix="/api/v1/events", tags=["Events"])

@router.post("")
async def ingest_events(
    request: Request,
    authorization: Optional[str] = Header(None)
):
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Invalid API Key")
    
    # Decompress GZIP payload body explicitly
    body_bytes = await request.body()
    try:
        decompressed = gzip.decompress(body_bytes)
        payload_data = json.loads(decompressed)
        payload = BatchPayload(**payload_data)
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"Failed to decompress or parse JSON: {e}")
    
    # Open DB Session using the shared engine
    engine = request.app.state.engine
    
    acked_ids = []
    
    with Session(engine) as session:
        for evt in payload.events:
            # Convert python dicts back to generic JSON for SQLite
            db_event = RawEvent(
                id=evt.event_id,
                user_id=payload.user_id,
                device_id=payload.device_id,
                session_id=evt.session_id,
                event_type=evt.type,
                ts_start=evt.ts_start,
                ts_end=evt.ts_end,
                active_ms=evt.active_duration_ms,
                context=evt.context.model_dump(),
                payload=evt.payload,
                processed=False
            )
            session.add(db_event)
            acked_ids.append(evt.event_id)
        
        # Commit the transaction block synchronously for safety
        session.commit()
    
    print(f"Server successfully received and saved batch {payload.batch_id} with {len(acked_ids)} events.")

    return {
        "status": "success",
        "batch_id": payload.batch_id,
        "ack_ids": acked_ids
    }
