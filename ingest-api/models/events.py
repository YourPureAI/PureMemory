from datetime import datetime
from typing import Any, Dict, List, Optional
from pydantic import BaseModel, Field, field_validator
import json

class EventContext(BaseModel):
    app_bundle: Optional[str] = ""
    app_name: Optional[str] = ""
    window_title: Optional[str] = ""
    url: Optional[str] = None
    url_domain: Optional[str] = None
    os_platform: Optional[str] = "windows"

class Event(BaseModel):
    event_id: str
    user_id: Optional[str] = ""
    session_id: Optional[str] = ""
    focus_session_id: Optional[str] = Field(default="", alias="focus_id")
    device_id: Optional[str] = ""
    schema_version: Optional[str] = "1.0"
    ts_start: Optional[datetime] = None
    ts_end: Optional[datetime] = None
    active_duration_ms: Optional[int] = 0
    idle_duration_ms: Optional[int] = 0
    tz: Optional[str] = "UTC"
    type: str
    context: Optional[EventContext] = Field(default_factory=EventContext)
    payload: Optional[Any] = None

    model_config = {"populate_by_name": True}

class BatchPayload(BaseModel):
    batch_id: str
    user_id: str
    device_id: str
    events: List[Event]
