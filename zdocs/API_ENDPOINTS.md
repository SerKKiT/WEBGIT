# 🚀 **COMPLETE CYCLE - SEQUENTIAL CURL COMMANDS**

Here's the complete sequential list of curl commands for a full platform test cycle:

## **1. SYSTEM HEALTH VERIFICATION**
```bash
curl -X GET "http://localhost/health"
curl -X GET "http://localhost/api/auth/health"
curl -X GET "http://localhost/api/main/health"
curl -X GET "http://localhost/api/stream/health"
curl -X GET "http://localhost/api/vod/health"
```

## **2. USER REGISTRATION**
```bash
curl -X POST "http://localhost/api/auth/register" -H "Content-Type: application/json" -d "{\"username\":\"testuser$(date +%s)\",\"email\":\"test$(date +%s)@test.local\",\"password\":\"test123456\",\"role\":\"streamer\"}"
```

## **3. TOKEN VALIDATION**
```bash
# Extract token from registration response and set it
export TOKEN="your_extracted_token_here"
curl -X POST "http://localhost/api/auth/validate-token" -H "Content-Type: application/json" -d "{\"token\":\"$TOKEN\"}"
```

## **4. STREAM CREATION**
```bash
curl -X POST "http://localhost/api/streams" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d "{\"name\":\"Complete Test Stream\",\"title\":\"Full Cycle Test - $(date)\"}"
```

## **5. STREAM START**
```bash
# Extract STREAM_ID from creation response
export STREAM_ID="your_extracted_stream_id"
curl -X POST "http://localhost/api/streams/$STREAM_ID/start" -H "Authorization: Bearer $TOKEN"
```

## **6. TASK CREATION**
```bash
curl -X POST "http://localhost/tasks" -H "Content-Type: application/json" -d "{\"name\":\"Test Task - $(date +%s)\",\"stream_id\":\"$STREAM_ID\",\"status\":\"pending\"}"
```

## **7. HLS PLAYLIST CHECK**
```bash
curl -X GET "http://localhost/hls/$STREAM_ID/stream.m3u8"
```

## **8. STREAM STOP**
```bash
curl -X POST "http://localhost/api/streams/$STREAM_ID/stop" -H "Authorization: Bearer $TOKEN"
```

## **9. VOD PROCESSING CHECK**
```bash
# Wait 60 seconds for processing
sleep 60
curl -X GET "http://localhost/api/recordings/$STREAM_ID"
```

## **10. THUMBNAIL ACCESS**
```bash
curl -X GET "http://localhost/api/recordings/$STREAM_ID/thumbnail" -o "test_thumbnail.jpg"
```

## **11. CLEANUP OPERATIONS**
```bash
# Clean up tasks (correct query parameter format)
curl -X DELETE "http://localhost/tasks?id=1"
curl -X DELETE "http://localhost/tasks?id=2"
curl -X DELETE "http://localhost/tasks?id=3"

# Update task status by stream
curl -X PUT "http://localhost/tasks/update_status_by_stream" -H "Content-Type: application/json" -d "{\"stream_id\":\"$STREAM_ID\",\"status\":\"deleted\"}"

# Systematic cleanup (IDs 1-20)
for i in {1..20}; do curl -X DELETE "http://localhost/tasks?id=$i"; done
```

## **12. FINAL VERIFICATION**
```bash
curl -X GET "http://localhost/tasks"
curl -X GET "http://localhost/api/streams/my" -H "Authorization: Bearer $TOKEN"
```

## **COMPLETE ONE-LINER SEQUENCE:**
```bash
curl -X GET "http://localhost/health" && curl -X POST "http://localhost/api/auth/register" -H "Content-Type: application/json" -d "{\"username\":\"test$(date +%s)\",\"email\":\"test$(date +%s)@test.local\",\"password\":\"test123456\",\"role\":\"streamer\"}" > reg.json && export TOKEN=$(cat reg.json | grep -o '"access_token":"[^"]*' | cut -d'"' -f4) && curl -X POST "http://localhost/api/streams" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d "{\"name\":\"Quick Test\",\"title\":\"One Liner Test\"}" > stream.json && export STREAM_ID=$(cat stream.json | grep -o '"stream_id":"[^"]*' | cut -d'"' -f4) && curl -X POST "http://localhost/api/streams/$STREAM_ID/start" -H "Authorization: Bearer $TOKEN" && sleep 10 && curl -X POST "http://localhost/api/streams/$STREAM_ID/stop" -H "Authorization: Bearer $TOKEN" && sleep 60 && curl -X GET "http://localhost/api/recordings/$STREAM_ID" && for i in {1..10}; do curl -X DELETE "http://localhost/tasks?id=$i" 2>/dev/null; done
```

This sequential command set provides a complete test cycle covering health checks, user management, streaming operations, VOD processing, and cleanup - everything needed to verify your Enterprise Streaming Platform is production-ready.