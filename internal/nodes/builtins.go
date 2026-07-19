package nodes

import (
	"github.com/n8n-io/n8n-turbo/internal/descriptor"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/nodes/cloudinary"
	"github.com/n8n-io/n8n-turbo/internal/nodes/discord"
	"github.com/n8n-io/n8n-turbo/internal/nodes/msteams"
	"github.com/n8n-io/n8n-turbo/internal/nodes/sendgrid"
	"github.com/n8n-io/n8n-turbo/internal/nodes/shopify"
	"github.com/n8n-io/n8n-turbo/internal/nodes/telegram"
	"github.com/n8n-io/n8n-turbo/internal/nodes/trello"
	"github.com/n8n-io/n8n-turbo/internal/nodes/twilio"
)

func RegisterBuiltins(registry engine.Registry) {
	descriptor.RegisterBuiltins(registry)
	registry.Register("n8n-nodes-base.start", ManualTrigger{})
	registry.Register("n8n-nodes-base.manualTrigger", ManualTrigger{})
	registry.Register("n8n-nodes-base.stickyNote", StickyNote{})
	registry.Register("n8n-nodes-base.noOp", NoOp{})
	registry.Register("n8n-nodes-base.set", Set{})
	registry.Register("n8n-nodes-base.editFields", Set{})
	registry.Register("n8n-nodes-base.if", If{})
	registry.Register("n8n-nodes-base.switch", Switch{})
	registry.Register("n8n-nodes-base.filter", Filter{})
	registry.Register("n8n-nodes-base.merge", Merge{})
	registry.Register("n8n-nodes-base.limit", Limit{})
	registry.Register("n8n-nodes-base.splitInBatches", SplitInBatches{})
	registry.Register("n8n-nodes-base.loopOverItems", LoopOverItems{})
	registry.Register("n8n-nodes-base.wait", Wait{})
	registry.Register("n8n-nodes-base.sort", Sort{})
	registry.Register("n8n-nodes-base.removeDuplicates", RemoveDuplicates{})
	registry.Register("n8n-nodes-base.splitOut", SplitOut{})
	registry.Register("n8n-nodes-base.aggregate", Aggregate{})
	registry.Register("n8n-nodes-base.summarize", Summarize{})
	registry.Register("n8n-nodes-base.dateTime", DateTime{})
	registry.Register("n8n-nodes-base.crypto", Crypto{})
	registry.Register("n8n-nodes-base.code", Code{})
	registry.Register("n8n-nodes-base.function", Code{})
	registry.Register("n8n-nodes-base.functionItem", Code{})
	registry.Register("n8n-nodes-base.executeCommand", ExecuteCommand{})
	registry.Register("n8n-nodes-base.executeWorkflow", ExecuteWorkflow{})
	registry.Register("n8n-nodes-base.readWriteFile", ReadWriteFile{})
	registry.Register("n8n-nodes-base.compression", Compression{})
	registry.Register("n8n-nodes-base.html", HTML{})
	registry.Register("n8n-nodes-base.xml", XML{})
	registry.Register("n8n-nodes-base.markdown", Markdown{})
	registry.Register("n8n-nodes-base.convertToFile", ConvertToFile{})
	registry.Register("n8n-nodes-base.extractFromFile", ExtractFromFile{})
	registry.Register("n8n-nodes-base.webhook", Webhook{})
	registry.Register("n8n-nodes-base.formTrigger", Webhook{})
	registry.Register("n8n-nodes-base.errorTrigger", ErrorTrigger{})
	registry.Register("n8n-nodes-base.executeWorkflowTrigger", ExecuteWorkflowTrigger{})
	registry.Register("n8n-nodes-base.scheduleTrigger", ScheduleTrigger{})
	registry.Register("n8n-nodes-base.gmailTrigger", GmailTrigger{})
	registry.Register("n8n-nodes-cloudinary.cloudinary", cloudinary.New())
	registry.Register("n8n-nodes-base.respondToWebhook", RespondToWebhook{})
	registry.Register("n8n-nodes-base.httpRequest", HTTPRequest{})
	registry.Register("n8n-nodes-base.n8n", N8n{})
	registry.Register("@n8n/n8n-nodes-langchain.agent", AIAgent{})
	registry.Register("@n8n/n8n-nodes-langchain.lmChatGoogleGemini", GoogleGeminiChatModel{})
	registry.Register("@n8n/n8n-nodes-langchain.lmChatDeepSeek", DeepSeekChatModel{})
	registry.Register("@n8n/n8n-nodes-langchain.lmChatOpenRouter", OpenRouterChatModel{})
	registry.Register("n8n-nodes-base.telegram", telegram.New(nil))
	registry.Register("n8n-nodes-base.discord", discord.New())
	registry.Register("n8n-nodes-base.twilio", twilio.New())
	registry.Register("n8n-nodes-base.sendGrid", sendgrid.New(nil))
	registry.Register("n8n-nodes-base.shopify", shopify.New())
	registry.Register("n8n-nodes-base.microsoftTeams", msteams.New())
	registry.Register("n8n-nodes-base.trello", trello.New(nil))
	registry.Register("n8n-nodes-base.sqlite", SQLite{})
	registry.Register("n8n-nodes-base.postgres", Postgres{})
	registry.Register("n8n-nodes-base.mySql", MySQL{})
	registry.Register("n8n-nodes-base.redis", Redis{})
	registry.Register("n8n-nodes-base.mongoDb", MongoDB{})
}
