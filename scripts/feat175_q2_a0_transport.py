"""Bounded HTTP/SSE transport shared by loopback qualification and gated live I/O."""
import hashlib
import http.client
import json
import queue
import socket
import threading
import time
from urllib.parse import urlsplit

from feat175_q2_a0_policy import POLICY
import feat175_q2_meter as v1
import feat175_q2_v2_meter as v2


def deadline(started, closed_at, limits):
    absolute = started + limits["http_absolute"]
    return min(absolute, closed_at + limits["drain_after_close"]) if closed_at is not None else absolute


class Observer(v1.Observer):
    def __init__(self, meter, row):
        super().__init__()
        self.meter, self.row, self.event_buffer = meter, row, b""
        self.events_invalid = False

    def feed(self, data):
        super().feed(data)
        if self.events_invalid:return
        self.event_buffer += data
        if len(self.event_buffer)>2*1024*1024:
            self.event_buffer=b"";self.events_invalid=True;return
        while b"\n" in self.event_buffer:
            line,self.event_buffer=self.event_buffer.split(b"\n",1)
            if not line.startswith(b"data:") or line[5:].strip()==b"[DONE]":continue
            try:
                event=json.loads(line[5:]);items=[]
                stage=event.get("type","")
                if stage in ("response.output_item.added","response.output_item.done"):
                    items=[event["item"]]
                elif stage in ("response.completed","response.incomplete","response.failed"):
                    items=event.get("response",{}).get("output",[])
                for item in items:
                    if item.get("type")!="web_search_call":continue
                    identity=item.get("id")
                    if not isinstance(identity,str) or not 0<len(identity)<=128:raise ValueError("ID")
                    with self.meter.lock:
                        if len(self.row["provider_events"])>=256:raise ValueError("Bound")
                        task=self.meter.state["tasks"][self.row["task_index"]]
                        status=item.get("status")
                        if status not in (None,"completed","failed","in_progress","searching"):status="unknown"
                        record=self.meter.journal("provider_item_observed",task_index=self.row["task_index"],
                            ordinal=self.row["ordinal"],item_id=identity,stage=stage,status=status,after_cutoff=task["root_closed"])
                        self.row["provider_events"].append(record)
                        self.meter.persist()
            except (ValueError,TypeError,KeyError,AttributeError):
                self.events_invalid=True


def relay(handler, meter, row, payload, keys, *, local_url=None):
    limits=POLICY["deadlines_seconds"]
    # Mode is set by the guarded entry point, not a model request or URL argument.
    if meter.execution_mode=="live":
        v1.require(local_url is None,"Live transport cannot use a caller endpoint")
        host=v1.MODELS[row["model"]][0]
        connection=http.client.HTTPSConnection(host,timeout=15)
        headers={"Authorization":"Bearer "+keys[row["model"]],"Content-Type":"application/json"}
    else:
        u=urlsplit(local_url or "")
        v1.require(u.scheme=="http" and u.hostname=="127.0.0.1" and u.port and not u.username and not u.query and not u.fragment,"Owned loopback upstream required")
        connection=http.client.HTTPConnection(u.hostname,u.port,timeout=15)
        headers={"Authorization":"Bearer ordinary-local-upstream","Content-Type":"application/json",
                 "X-Q2-Case":row["case_id"],"X-Q2-Ordinal":str(row["ordinal"]),
                 "X-Q2-Role":"root" if row["thread_id"]==row["root_thread_id"] else "child",
                 "X-Q2-Step":str(sum(r["thread_id"]==row["thread_id"] and r["turn_id"]==row["turn_id"] for r in meter.state["requests"]))}
    chunks=queue.Queue(maxsize=8);stop=threading.Event()
    def put(value):
        while not stop.is_set():
            try:chunks.put(value,timeout=.1);return
            except queue.Full:pass
    def read_upstream():
        try:
            connection.request("POST","/v1/responses",body=payload,headers=headers)
            if connection.sock:connection.sock.settimeout(limits["http_idle"])
            response=connection.getresponse()
            put(("headers",(response.status,response.getheader("Content-Type",""),response.getheader("Content-Encoding"))))
            while not stop.is_set():
                chunk=response.read1(65536)
                if not chunk:put(("eof",None));return
                put(("data",chunk))
        except (OSError,http.client.HTTPException):put(("error","upstream_transport_error"))
        finally:connection.close()
    meter.dispatch(row)
    reader=threading.Thread(target=read_upstream)
    reader.start()
    observer=Observer(meter,row);response_hash=hashlib.sha256();count=0;complete=False;connected=True
    started=time.monotonic();last_data=started;error_bytes=bytearray();reason=None;sent_headers=False
    try:
        while True:
            with meter.lock:closed_at=meter.state["tasks"][row["task_index"]].get("closed_at")
            current=time.monotonic();remaining=min(deadline(started,closed_at,limits)-current,limits["http_idle"]-(current-last_data))
            if remaining<=0:
                reason="stream_deadline" if current>=deadline(started,closed_at,limits) else "stream_idle"
                break
            try:kind,value=chunks.get(timeout=min(.1,remaining))
            except queue.Empty:continue
            if kind=="headers":
                status,content_type,encoding=value
                with meter.lock:row["http_status"]=status
                try:
                    handler.send_response(status);handler.send_header("Content-Type",content_type)
                    if encoding:handler.send_header("Content-Encoding",encoding)
                    handler.send_header("Connection","close");handler.end_headers();sent_headers=True
                except OSError:connected=False
            elif kind=="data":
                last_data=time.monotonic();response_hash.update(value);count+=len(value);observer.feed(value)
                if row.get("http_status",0)>=400:error_bytes.extend(value[:max(0,16385-len(error_bytes))])
                if connected:
                    try:handler.wfile.write(value);handler.wfile.flush()
                    except OSError:connected=False # Continue reading real usage; no resend.
            elif kind=="eof":complete=True;break
            else:reason="unknown_outcome";break
    finally:
        stop.set()
        # Normal cancellation of this owned HTTP operation at its declared
        # deadline. No process signals, force-kill or network fault injection.
        if reader.is_alive():
            try:
                if connection.sock:connection.sock.shutdown(socket.SHUT_RDWR)
            except OSError:pass
            connection.close()
        reader.join(2)
        handler.close_connection=True
        if not sent_headers and connected:
            try:handler.reply(502,{"error":{"message":"Bounded provider response unavailable"}})
            except OSError:pass
        observer.result.update(stream_complete=complete,observer_valid=not observer.invalid and not observer.extra_invalid
            and not observer.events_invalid and not observer.buffer.strip() and not observer.extra_buffer.strip() and not observer.event_buffer.strip(),
            response_sha256=response_hash.hexdigest(),response_bytes=count,client_connected_at_eof=connected,
            upstream_reader_stopped=not reader.is_alive())
        if row.get("http_status",0)>=400:
            observer.result.update(v1.shared.error_metadata(error_bytes));reason="provider_http_error"
        if any(e.get("status")=="failed" for e in row["provider_events"]):reason="provider_tool_error"
        if reader.is_alive():reason="unknown_outcome"
        if reason:
            observer.result["stop_code"]=reason;meter.halt(reason)
        meter.settle(row,observer.result)


def response_bytes(body,row,scenario):
    """Explicit ordinary local protocol producer; never called by live transport."""
    role=row.get("role","root");step=row["case_request_number"]
    raw=v2.response_fixture(body,row,"complete")
    events=[json.loads(line[5:]) for line in raw.splitlines() if line.startswith(b"data:")]
    output=events[-1]["response"]["output"]
    if scenario in ("children","late-binding") and row["case_id"]=="Q2-03":
        if role=="root":
            if step==1:
                output=[{"type":"function_call","name":"collaboration__spawn_agent","call_id":"a0_spawn_"+str(row["ordinal"]),
                         "arguments":json.dumps({"task_name":"a0_child","message":"A0 ordinary local child research task. Read the assigned public documentation.","fork_turns":"all"})}]
            elif scenario=="children":
                output=[{"type":"function_call","name":"collaboration__wait_agent","call_id":"a0_wait_"+str(row["ordinal"]),"arguments":json.dumps({"timeout_ms":10000})}]
            else:
                # Parent performs its ordinary discovery/retrieval while child
                # native-binding delivery has a short normal readiness delay.
                changed={**row,"case_request_number":step-1}
                source=v2.response_fixture(body,changed,"complete")
                output=json.loads([s for s in source.splitlines() if s.startswith(b"data:")][-1][5:])["response"]["output"]
        # Child follows the original Kimi tool_search/exec or MiniMax direct path.
    for i,item in enumerate(output):item["id"]=f"a0_item_{row['ordinal']}_{i}"
    events[-1]["response"]["output"]=output
    rebuilt=[events[0]]
    for i,item in enumerate(output):rebuilt.extend({"type":kind,"output_index":i,"item":item} for kind in ("response.output_item.added","response.output_item.done"))
    rebuilt.append(events[-1])
    return [b"data: "+json.dumps(e).encode()+b"\n\n" for e in rebuilt]


def local_upstream(meter,scenario):
    import http.server
    class Peer(http.server.BaseHTTPRequestHandler):
        def log_message(self,*_):pass
        def do_POST(self):
            raw=self.rfile.read(int(self.headers["Content-Length"]))
            body=json.loads(raw)
            row={"case_id":self.headers["X-Q2-Case"],"ordinal":int(self.headers["X-Q2-Ordinal"]),"model":body["model"],
                 "role":self.headers["X-Q2-Role"],"case_request_number":int(self.headers["X-Q2-Step"])}
            with meter.lock:
                meter.state.setdefault("local_upstream_receipts",[]).append({"ordinal":row["ordinal"],"request_sha256":v1.digest(raw),"bytes":len(raw)})
                meter.persist()
            parts=response_bytes(body,row,scenario)
            self.send_response(200);self.send_header("Content-Type","text/event-stream");self.send_header("Connection","close");self.end_headers()
            try:
                for i,part in enumerate(parts):
                    self.wfile.write(part);self.wfile.flush()
                    if i==0 and scenario=="cancel-tail":
                        meter.halt("owner_stop")
                        time.sleep(20) # normal delayed terminal, beyond a short control-RPC timeout.
                    else:time.sleep(.005)
            except (BrokenPipeError,ConnectionResetError):pass
            self.close_connection=True
    return http.server.ThreadingHTTPServer(("127.0.0.1",0),Peer)
