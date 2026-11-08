// Code generated from the canonical FEAT-169 candidate. DO NOT EDIT.
package runtimenativetools

import _ "embed"

const RuntimeManifestSHA256 = "1c826988d03dd0069b3a52a6f85d8c02b1ce196d5755ff25666f6573a6186144"
const RuntimeBinarySHA256 = "7a4f7f4eb051f7a184bad2a9d75b9845cda271228586ad60fb8e735f5fdd4a64"
const RuntimeBinarySize int64 = 357377832
const RuntimeSchemaTreeSHA256 = "79b27b23a58249dc700567c4c0c824c222bbf4c5b81b894dea5fe8d84f34eb78"
const RuntimeSchemaFileCount = 271
const RuntimePatchManifest = "[{\"path\":\".yijie/patches/0001-feat-126-filter-persistent-diagnostics.patch\",\"sha256\":\"6b337a02caf064c6819fab5c7367a485004c85cce0d42acb06fa6d5003e599a0\"},{\"path\":\".yijie/patches/0002-feat-136-unified-exec-pre-emitter-command-lifecycle.patch\",\"sha256\":\"43de168e1443f4b9ca60d7f61e3de2daf20e1cfea14d2e196d28ba417bf3e06d\"},{\"path\":\".yijie/patches/input-only/0003-input-only-execution.patch\",\"sha256\":\"c340b6fc17419bcdc1ff45eebe3da40e5fde740c029102eb7a188f6b14eb41d8\"},{\"path\":\".yijie/patches/chat-models/0004-feat-156-upstream-tool-choice.patch\",\"sha256\":\"828f913fead29da08c015d73ef6c7102a8adf9afd3811ab5469891ec5efc2abf\"},{\"path\":\".yijie/patches/chat-models/0005-feat-156-terminal-tool-arguments.patch\",\"sha256\":\"f1d6689f44056c252530ad940bb253e8a0ace60e810b88857440699ba908917e\"},{\"path\":\".yijie/patches/chat-models/0006-native-web-search-sources.patch\",\"sha256\":\"8fdb9d2f7d7708a603a8d08906a4d3cd6ce098ca9165471f95cb0816ed6718ee\"},{\"path\":\".yijie/patches/chat-models/0007-feat-164-kimi-k31-pro.patch\",\"sha256\":\"c666c066ec1235d016e7479805242d049def542b9f8c684970ac8e5b4cee8ae6\"},{\"path\":\".yijie/patches/chat-models/0008-feat-164-kimi-responses-include.patch\",\"sha256\":\"c3dc2b58e6ae9d1a2335bc4937defbbaf9da99848575d1e554fc579ef9aec475\"},{\"path\":\".yijie/patches/chat-models/0009-feat-164-native-failure-terminals.patch\",\"sha256\":\"ae74c11488a31644e9b052f1864fd4a76ecdc05e9019206efd2e7d82f8595886\"},{\"path\":\".yijie/patches/chat-models/0010-feat-164-sanitized-persistence-metrics.patch\",\"sha256\":\"bcffb5337101c7d2032acaa0dc633cd92f2d12110eff0be10a79884f38eb85a7\"},{\"path\":\".yijie/patches/chat-models/0011-feat-164-graceful-state-close.patch\",\"sha256\":\"b5054bce23e12f5638cddbf54f4936a8116d62f6ba62208efa58132580c95e83\"},{\"path\":\".yijie/patches/chat-models/0012-web-retrieval-execution-guard.patch\",\"sha256\":\"235fc906fe30868fab8135909701f6bdaf21f422eb90959ea10de4bd58c1ff09\"},{\"path\":\".yijie/patches/chat-models/0013-answer-completion.patch\",\"sha256\":\"36b9b9b245971493120b3f6d334ef7daa15b310b94b11de1440d28a4c2c1ce73\"},{\"path\":\".yijie/patches/chat-models/0014-execution-progress.patch\",\"sha256\":\"453ca48015787976b063ebc89c56965a7cdfa82fb980c98f03c57216cbd0940b\"},{\"path\":\".yijie/patches/chat-models/0015-public-progress-completion.patch\",\"sha256\":\"41e8fd2e1a9497d5424e50307aa31b7114c632a8e59c7c7adb2acb5cd0a12ba4\"},{\"path\":\".yijie/patches/chat-models/0016-native-public-messages.patch\",\"sha256\":\"2351e6a7941f3cf24a4f1efe2ec40b6f43254d320c3ae09469cbf18e64e86945\"},{\"path\":\".yijie/patches/chat-models/0017-native-public-message-validation.patch\",\"sha256\":\"392a4c4141cabedcad148e2ce9b0133a64287152cefb16c576295409da9bb84b\"},{\"path\":\".yijie/patches/chat-models/0018-native-public-message-spelling.patch\",\"sha256\":\"91deb2b8fda2c6e2d0f64a835f5fcf30930715c44e63943a763419a39276dff2\"},{\"path\":\".yijie/patches/chat-models/0019-host-image-generation.patch\",\"sha256\":\"5c202edda265bdec4a8af87ed4d1b7101ab4e4fbb3e48f1fa59bb09947272f6a\"},{\"path\":\".yijie/patches/chat-models/0020-native-tool-integration.patch\",\"sha256\":\"0dabd01b0e3d9195eb5b949999a075c3bb5183a4a1586bb2eb1ede5c5b0920a2\"},{\"path\":\".yijie/patches/chat-models/0021-native-tool-policy-audit.patch\",\"sha256\":\"c210fd77202411c8f6b4827296284e6a62e19005176af11780fcb64ce94b46a8\"}]"

//go:embed profile.json
var Profile []byte

//go:embed config.schema.json
var ConfigSchema []byte

//go:embed thread-read.schema.json
var ThreadReadSchema []byte

// These selected fields are checked against the canonical ThreadReadResponse.
// Unknown canonical fields are intentionally ignored, never treated as authority.
type ThreadIdentity struct {
	ID             string         `json:"id"`
	ParentThreadID *string        `json:"parentThreadId"`
	Turns          []TurnIdentity `json:"turns"`
}
type TurnIdentity struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
type ThreadReadProjection struct {
	Thread ThreadIdentity `json:"thread"`
}
