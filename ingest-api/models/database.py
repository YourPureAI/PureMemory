from sqlalchemy import Column, String, DateTime, Boolean, BigInteger, func, JSON
from sqlalchemy.orm import declarative_base

Base = declarative_base()

class Device(Base):
    __tablename__ = 'devices'

    device_id = Column(String, primary_key=True)
    user_id = Column(String, nullable=False, index=True)
    api_key_hash = Column(String, nullable=False)
    os_platform = Column(String, nullable=False)
    hostname = Column(String)
    registered_at = Column(DateTime(timezone=True), server_default=func.now())
    last_seen_at = Column(DateTime(timezone=True))

class RawEvent(Base):
    __tablename__ = 'raw_events'

    id = Column(String, primary_key=True) # ULID from agent
    user_id = Column(String, nullable=False, index=True)
    device_id = Column(String, nullable=False, index=True)
    session_id = Column(String, nullable=False)
    received_at = Column(DateTime(timezone=True), server_default=func.now(), index=True)
    event_type = Column(String, nullable=False, index=True)
    ts_start = Column(DateTime(timezone=True), nullable=False)
    ts_end = Column(DateTime(timezone=True))
    active_ms = Column(BigInteger)
    context = Column(JSON, nullable=False)
    payload = Column(JSON, nullable=False)
    processed = Column(Boolean, default=False, index=True)

class MemoryEntry(Base):
    __tablename__ = 'memory_entries'
    
    id = Column(BigInteger, primary_key=True, autoincrement=True)
    user_id = Column(String, nullable=False, index=True)
    device_id = Column(String, nullable=False, index=True)
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    period_start = Column(DateTime(timezone=True), nullable=False)
    period_end = Column(DateTime(timezone=True), nullable=False)
    entry_type = Column(String, nullable=False, index=True)
    title = Column(String)
    summary = Column(String, nullable=False)
    tags = Column(JSON) # Fallback from PG ARRAY
    raw_event_ids = Column(JSON) # Fallback from PG ARRAY
    metadata_blob = Column(JSON)
