import secrets
from fastapi import APIRouter, HTTPException, Depends
from pydantic import BaseModel
from passlib.context import CryptContext

router = APIRouter(prefix="/api/v1/devices", tags=["Devices"])
pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")

class RegisterRequest(BaseModel):
    device_id: str
    os: str
    hostname: str

class RegisterResponse(BaseModel):
    status: str
    api_key: str

@router.post("/register", response_model=RegisterResponse)
async def register_device(req: RegisterRequest):
    # In a real environment, DB lookup logic occurs here to check if device exists.
    # For now, we simulate securely issuing an API Key.
    raw_api_key = secrets.token_urlsafe(32)
    hashed_key = pwd_context.hash(raw_api_key)
    
    # Example logic: session.add(Device(device_id=req.device_id, api_key_hash=hashed_key...))
    print(f"Registered new {req.os} device: {req.device_id}")

    return RegisterResponse(
        status="ok",
        api_key=raw_api_key
    )
