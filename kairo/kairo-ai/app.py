# kairo-api/app.py
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import Optional
import uvicorn
import datetime
import asyncio

# Mock Portkey AI Gateway call function (replace with actual API call)
async def call_portkey_ai(event_data: dict) -> dict:
    # Simulate delay
    await asyncio.sleep(1)
    # Dummy response: normally you'd call Portkey API here
    return {
        "root_cause": f"Simulated root cause for pod {event_data['pod_name']}",
        "fix_recommendation": "Restart pod or check image version.",
        "failure_started_at": event_data.get("timestamp", "unknown"),
    }

app = FastAPI(title="Kairo RCA API")

class PodFailureEvent(BaseModel):
    pod_name: str
    namespace: str
    reason: str
    message: Optional[str] = None
    timestamp: Optional[str] = None  # ISO8601 string

@app.post("/analyze")
async def analyze_failure(event: PodFailureEvent):
    try:
        # Validate/parse timestamp or set now
        ts = event.timestamp or datetime.datetime.utcnow().isoformat()
        
        # Prepare data for Portkey AI Gateway call
        event_data = event.dict()
        event_data["timestamp"] = ts
        
        # Call Portkey AI Gateway (mock)
        rca_result = await call_portkey_ai(event_data)
        
        # Return RCA info
        return {
            "pod_name": event.pod_name,
            "namespace": event.namespace,
            "reason": event.reason,
            "timestamp": ts,
            "root_cause": rca_result["root_cause"],
            "fix_recommendation": rca_result["fix_recommendation"],
            "failure_started_at": rca_result["failure_started_at"],
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

if __name__ == "__main__":
    uvicorn.run("app:app", host="0.0.0.0", port=8000, reload=True)
