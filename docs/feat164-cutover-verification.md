# FEAT-164 loaded legacy and automatic compaction checks

Contract impact: none. This adds explicitly opted-in tests and an offline runner; product sources, public schemas, managed model admission and Runtime artifacts are unchanged. Product source remains cac2d4d7e1df02868a370e204959e1842c3362c3. The earlier S4c App checkout stays at that Host pin.

Use scripts/feat164-s4d-conformance.py with explicit output directory, installed Go1.26.5 binary, current canonical r2 artifact pair and retained chat-models-web-sources artifact pair. It runs six independent Go cases (including subtests), requires zero skips, fourteen loopback requests and normal shutdown. It does not authorize external provider calls. Existing native config arguments route both process providers to loopback; dummy credentials and retry=0 are mandatory and effective config is checked before turns. No caller provider settings are inherited.

The legacy producer creates old parent/child history normally, proves both loaded and completed, shuts the complete Runtime down normally, then the candidate explicitly resumes the same native IDs with K3.1 Pro/max. Legacy loopback calls occur before the cutoff and are counted separately. No old binary is started on a new product database. New and migrated long text trigger native automatic compaction; no explicit compact API substitutes for that observation. Synthetic usage is calculated from fixture text size and never reported as vendor usage or cost.

Ordinary compatibility catalog fixtures additionally exercise changed comp_hash, smaller windows, and an old identity in an already loaded native child. They are not the product catalog, which still excludes K3. The two previous-model branches currently reject at the retired request guard with zero post-cutoff HTTP; they are safety rejections, not successful migrations of those hypothetical configurations.

## Known failure retained

TestFEAT164LoadedRetiredAgentGuard is intentionally still failing against current r2: the native error notifications show three retired-model errors (old parent, child first turn, loaded-child follow-up), and the meter sees only six current parent requests. However wait_agent returns completed:null and thread/read reconstructs two completed child turns containing only userMessage. This is not model success. Core agent status derives Error as Errored and later TurnComplete as Completed(None); session delivery replaces the former. The separate app-server live turn error accumulator does not make child wait status or reconstructed history equivalent.

The first attempt additionally had a catalog prerequisite error; two subsequent observations confirm the status defect. Three successive child acceptance attempts are retained and no further retry was made. The independent runner excludes this unresolved case explicitly; its success cannot qualify full TCONF-164-04 or S4/G3. See the meta FEAT-164 report30 for the RCA and authorization boundary. Do not weaken the assertion, reinterpret completed:null as success, or patch upstream core without the required design/Runtime approval.

All checks use normal protocol inputs, synthetic local text, standard canonical binaries and normal shutdown. No forced kill, executable substitution, permission corruption, attack fixture or user database mutation is allowed.
