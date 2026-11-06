// Code generated from the fixed app-server image candidate. DO NOT EDIT.
package runtimehostimages

const Method = "item/imageGeneration/generate"
const RuntimeManifestSHA256 = "620353f307127685158ef3312dd7465b9d976bc2055551ee3b12b3ace86db6ff"
const RuntimeBinarySHA256 = "0d2f1078b0e413e22e22a30483f53cc2b4496f682db733e182fb28462d6903b9"
const RuntimeBinarySize int64 = 357431528
const RuntimeSchemaTreeSHA256 = "79b27b23a58249dc700567c4c0c824c222bbf4c5b81b894dea5fe8d84f34eb78"
const RuntimeSchemaFileCount = 271
const RuntimePatchManifest = "[{\"path\":\".yijie/patches/0001-feat-126-filter-persistent-diagnostics.patch\",\"sha256\":\"6b337a02caf064c6819fab5c7367a485004c85cce0d42acb06fa6d5003e599a0\"},{\"path\":\".yijie/patches/0002-feat-136-unified-exec-pre-emitter-command-lifecycle.patch\",\"sha256\":\"43de168e1443f4b9ca60d7f61e3de2daf20e1cfea14d2e196d28ba417bf3e06d\"},{\"path\":\".yijie/patches/input-only/0003-input-only-execution.patch\",\"sha256\":\"c340b6fc17419bcdc1ff45eebe3da40e5fde740c029102eb7a188f6b14eb41d8\"},{\"path\":\".yijie/patches/chat-models/0004-feat-156-upstream-tool-choice.patch\",\"sha256\":\"828f913fead29da08c015d73ef6c7102a8adf9afd3811ab5469891ec5efc2abf\"},{\"path\":\".yijie/patches/chat-models/0005-feat-156-terminal-tool-arguments.patch\",\"sha256\":\"f1d6689f44056c252530ad940bb253e8a0ace60e810b88857440699ba908917e\"},{\"path\":\".yijie/patches/chat-models/0006-native-web-search-sources.patch\",\"sha256\":\"8fdb9d2f7d7708a603a8d08906a4d3cd6ce098ca9165471f95cb0816ed6718ee\"},{\"path\":\".yijie/patches/chat-models/0007-feat-164-kimi-k31-pro.patch\",\"sha256\":\"c666c066ec1235d016e7479805242d049def542b9f8c684970ac8e5b4cee8ae6\"},{\"path\":\".yijie/patches/chat-models/0008-feat-164-kimi-responses-include.patch\",\"sha256\":\"c3dc2b58e6ae9d1a2335bc4937defbbaf9da99848575d1e554fc579ef9aec475\"},{\"path\":\".yijie/patches/chat-models/0009-feat-164-native-failure-terminals.patch\",\"sha256\":\"ae74c11488a31644e9b052f1864fd4a76ecdc05e9019206efd2e7d82f8595886\"},{\"path\":\".yijie/patches/chat-models/0010-feat-164-sanitized-persistence-metrics.patch\",\"sha256\":\"bcffb5337101c7d2032acaa0dc633cd92f2d12110eff0be10a79884f38eb85a7\"},{\"path\":\".yijie/patches/chat-models/0011-feat-164-graceful-state-close.patch\",\"sha256\":\"b5054bce23e12f5638cddbf54f4936a8116d62f6ba62208efa58132580c95e83\"},{\"path\":\".yijie/patches/chat-models/0012-web-retrieval-execution-guard.patch\",\"sha256\":\"235fc906fe30868fab8135909701f6bdaf21f422eb90959ea10de4bd58c1ff09\"},{\"path\":\".yijie/patches/chat-models/0013-answer-completion.patch\",\"sha256\":\"36b9b9b245971493120b3f6d334ef7daa15b310b94b11de1440d28a4c2c1ce73\"},{\"path\":\".yijie/patches/chat-models/0014-execution-progress.patch\",\"sha256\":\"453ca48015787976b063ebc89c56965a7cdfa82fb980c98f03c57216cbd0940b\"},{\"path\":\".yijie/patches/chat-models/0015-public-progress-completion.patch\",\"sha256\":\"41e8fd2e1a9497d5424e50307aa31b7114c632a8e59c7c7adb2acb5cd0a12ba4\"},{\"path\":\".yijie/patches/chat-models/0016-native-public-messages.patch\",\"sha256\":\"2351e6a7941f3cf24a4f1efe2ec40b6f43254d320c3ae09469cbf18e64e86945\"},{\"path\":\".yijie/patches/chat-models/0017-native-public-message-validation.patch\",\"sha256\":\"392a4c4141cabedcad148e2ce9b0133a64287152cefb16c576295409da9bb84b\"},{\"path\":\".yijie/patches/chat-models/0018-native-public-message-spelling.patch\",\"sha256\":\"91deb2b8fda2c6e2d0f64a835f5fcf30930715c44e63943a763419a39276dff2\"},{\"path\":\".yijie/patches/chat-models/0019-host-image-generation.patch\",\"sha256\":\"5c202edda265bdec4a8af87ed4d1b7101ab4e4fbb3e48f1fa59bb09947272f6a\"}]"

type HostImageGenerateParams struct {
	CallID              string `json:"callId"`
	Prompt              string `json:"prompt"`
	ThreadID            string `json:"threadId"`
	TurnID              string `json:"turnId"`
	UseCurrentReference bool   `json:"useCurrentReference"`
}
type HostImageGenerateResponse struct {
	Success bool `json:"success"`
}
type ExpertImageRoute struct{ Route, ExpertID, Version, RoleSHA256, Service, Proxy, Method string }

var ExpertImageRoutes = [...]ExpertImageRoute{
	{"linkfox-mannequin-v1", "linkfox-expert-aigc-imagegen-mannequin-to-model", "0.1.0", "042a9acb59429f68adccd0a925fa9d83ea8ba1890b2a1b3f23ecd9049703b80c", "linkfox", "proxy_call", "aigc/imageGenAsync"},
}
