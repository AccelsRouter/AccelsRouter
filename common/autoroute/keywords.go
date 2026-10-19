package autoroute

// Default keyword lists.
//
// The English base lists are LiteLLM's complexity router defaults
// (litellm/router_strategy/complexity_router/config.py), used under the MIT
// License, Copyright (c) 2023 Berri AI. LiteLLM ships no Chinese keywords, so
// the Chinese lists are this project's own, and so are the English words
// listed under "additions": they widen recall and can be deleted without
// affecting the LiteLLM base. Where a word in an addition overlaps one of
// LiteLLM's, LiteLLM's form is the one kept (for example "analyze this"
// rather than a bare "analyze").
//
// A Chinese keyword needs at least two characters and must not be an everyday
// word on its own (bare 分析 or 方法 would fire on half of all Chinese prompts).
// That rule is enforced by Config.Validate.

var litellmCodeKeywords = []string{
	"function", "class", "def", "const", "let", "var", "import", "export", "return", "async", "await",
	"try", "catch", "exception", "error", "debug", "api", "endpoint", "request", "response",
	"database", "sql", "query", "schema", "algorithm", "implement", "refactor", "optimize", "python",
	"javascript", "typescript", "java", "rust", "golang", "react", "vue", "angular", "node", "docker",
	"kubernetes", "git", "commit", "merge", "branch", "pull request",
}

var litellmReasoningKeywords = []string{
	"step by step", "think through", "let's think", "reason through", "analyze this", "break down",
	"explain your reasoning", "show your work", "chain of thought", "think carefully", "consider all",
	"evaluate", "pros and cons", "compare and contrast", "weigh the options", "logical", "deduce",
	"infer", "conclude",
}

var litellmTechnicalKeywords = []string{
	"architecture", "distributed", "scalable", "microservice", "machine learning", "neural network",
	"deep learning", "encryption", "authentication", "authorization", "performance", "latency",
	"throughput", "benchmark", "concurrency", "parallel", "threading", "memory", "cpu", "gpu",
	"optimization", "protocol", "tcp", "http", "grpc", "websocket", "container", "orchestration",
}

var litellmSimpleKeywords = []string{
	"what is", "what's", "define", "definition of", "who is", "who was", "when did", "when was",
	"where is", "where was", "how many", "how much", "yes or no", "true or false", "simple", "brief",
	"short", "quick", "hello", "hi", "hey", "thanks", "thank you", "goodbye", "bye", "okay",
}

// Additions beyond LiteLLM.
var extraCodeKeywords = []string{
	"stack trace", "regex", "unit test", "json", "yaml", "script", "code", "syntax", "variable",
	"array", "pointer", "compile", "compiler", "debugging", "bug",
}

// Additions beyond LiteLLM.
var extraReasoningKeywords = []string{
	"explain why", "reasoning", "prove", "proof", "derive", "trade-off", "trade-offs", "tradeoff",
	"tradeoffs", "justify", "critically", "in depth", "root cause", "rigorous",
}

// Additions beyond LiteLLM.
var extraTechnicalKeywords = []string{
	"consistency", "consensus", "transaction", "index", "cache", "cluster", "load balancer",
	"scalability", "complexity", "data structure", "gradient", "embedding", "terraform", "oauth",
	"jwt", "tls", "idempotent", "race condition", "deadlock", "memory leak", "sharding",
	"replication", "kafka", "redis", "postgres", "migration", "backpropagation", "microservices",
}

var chineseCodeKeywords = []string{
	"代码", "函数", "接口", "数据库", "编译", "报错", "调试", "重构", "算法", "前端", "后端", "部署", "正则", "脚本", "编程", "堆栈",
	"单元测试", "变量", "数组", "指针", "线程", "源码", "命令行", "代码审查",
}

var chineseReasoningKeywords = []string{
	"一步一步", "逐步", "分步骤", "推理", "推导", "证明", "论证", "深入分析", "详细分析", "仔细思考", "权衡", "利弊", "优缺点", "对比分析",
	"根因", "严谨", "批判性",
}

var chineseTechnicalKeywords = []string{
	"架构", "分布式", "微服务", "高并发", "一致性", "共识算法", "事务", "索引", "缓存", "集群", "负载均衡", "容器", "加密", "协议",
	"性能优化", "时间复杂度", "空间复杂度", "数据结构", "机器学习", "神经网络", "梯度", "向量", "幂等", "竞态", "死锁", "内存泄漏", "分库分表",
	"主从", "消息队列", "限流", "熔断", "高可用", "灰度发布", "反向传播",
}

var chineseSimpleKeywords = []string{
	"是什么", "什么是", "定义", "翻译", "你好", "谢谢", "怎么读", "什么意思", "讲个笑话", "简单介绍", "一句话", "谁是", "什么时候", "在哪里",
	"有多少", "是或否", "对还是错", "再见", "拜拜",
}

func defaultKeywords() Keywords {
	join := func(lists ...[]string) []string {
		var out []string
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	}
	return Keywords{
		Code:      join(litellmCodeKeywords, extraCodeKeywords, chineseCodeKeywords),
		Reasoning: join(litellmReasoningKeywords, extraReasoningKeywords, chineseReasoningKeywords),
		Technical: join(litellmTechnicalKeywords, extraTechnicalKeywords, chineseTechnicalKeywords),
		Simple:    join(litellmSimpleKeywords, chineseSimpleKeywords),
	}
}

// Multi-step patterns. The first four are LiteLLM's, compiled the same way
// (case-insensitive, "." does not cross a newline); the rest are this
// project's Chinese counterparts. Go's RE2 has no lookaround, which none of
// them need.
var defaultMultiStepPatterns = []string{
	`(?i)first.*?then`,
	`(?i)step\s*\d`,
	`\d+\.\s`,
	`(?i)[a-z]\)\s`,
	`(?s)首先.*(然后|接着|其次|最后)`,
	`第[一二三四五六七八九十0-9]+步`,
	`步骤\s*[一二三四五六七八九十0-9]`,
	`(?m)^\s*[0-9]+[、．)）]\s*\S`,
}
