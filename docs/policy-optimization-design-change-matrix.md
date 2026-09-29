# Safety Review 与 Policy Optimization 修改意见落实矩阵

Date: 2026-09-07

本矩阵把《Safety Review 与 Policy Optimization Agent 综合设计修改意见》视为设计变更需求，不视为运行指令。状态含义：`已接受` 表示按原意落实并可能增加更严格保护；`部分接受` 表示保留目标但收紧了实现权限；`不接受` 表示未进入设计。当前没有整项被拒绝。

## 编号 1-17：Safety Review

| # | 状态 | 实际落实 |
|---:|---|---|
| 1 | 已接受 | Safety Review Design 2/4 固定“事实 -> 候选 -> Rule -> 必要条件 -> 排除条件 -> 冲突处理 -> 类别 -> 标签”，禁止投票后补理由。 |
| 2 | 已接受 | A、B、Router 每条并行；Dual-Safe 还要求双方完整、Router 完整且双候选为空、独立性完整、无 policy gap。 |
| 3 | 已接受 | Router 仅输出可观察 Feature 与最多 3+3 候选；Response 禁止 method；候选不是违规证据。 |
| 4 | 已接受 | Expert one-category-per-call，是唯一 Category Establishment Gate；完整条件/排除矩阵和 scene evidence 均为硬条件。 |
| 5 | 已接受 | Arbiter 只能选择 Expert `established` 类别；全未建立/失败时不得生成 Unsafe，未决进入 quarantine。 |
| 6 | 已接受 | `attack_method` 与 `attack_domain` 正交；Prompt 可同时命中，Response 只能有 domain。 |
| 7 | 已接受 | Prompt/Response evidence ownership 隔离；Response 可把 Prompt 当只读上下文，但必须有自身非 `prompt_only` 证据。 |
| 8 | 已接受 | 稳定用工身份全部归 `occupation_discrimination`；从 `other_discrimination` 删除 employment arrangement/status/type，specific 优先。 |
| 9 | 已接受 | `P04B-DECISION-001` 记录经济社会身份是 Dataset Policy，并固定稳定身份、因果、严重/实质伤害与排除条件。 |
| 10 | 已接受 | `typical/borderline/hard_negative/variant` 成为正式 Policy；`uncertain` 不是 case_type，真正不可解进入 quarantine。 |
| 11 | 已接受 | P04-B V1 不输出 `risk_level`；Schema/validator/acceptance 均拒绝恢复该字段，未来启用需独立 Severity Policy。 |
| 12 | 已接受 | Gold 流程为 Top Model -> Candidate -> Human Approved -> Core；Candidate 永不作为 acceptance truth。 |
| 13 | 已接受 | Development 以 `source=human_reviewed` 真实确认案例为核心，Synthetic 仅补覆盖，带 case/rule/decision provenance。 |
| 14 | 已接受 | Regression 独立维护各 strata；每个已解决 Error Pattern 生成不可变、版本化 Regression Contract。 |
| 15 | 已接受 | Hidden Unsafe 要求 >=18/20 Unsafe、0 Safe、<=2 quarantine；不是只看 recall。 |
| 16 | 已接受 | 50 条保留为 P04-B V1 Pilot Gate；明确后续 Development/Hidden 各扩至 100-300+ 属 P2。 |
| 17 | 已接受 | 允许 Profile 共享 API env；preflight 可在 metadata/import 后，但必须在 item claim/semantic processing 前，保证 0 claim/decision。 |

## 编号 18-26：Agent、状态与 Skill Runtime

| # | 状态 | 实际落实 |
|---:|---|---|
| 18 | 已接受 | 新增独立 `policy-optimizer`，优化 Policy/Prompt/Rule/Gold/流程，不给生产行直接终审。 |
| 19 | 已接受 | 明确 Workflow Engine、Model Client、State、Skill Runtime、Context Builder、Artifact、Regression、Release，不抽象为一次 HTTP。 |
| 20 | 已接受 | 长期连续性只依赖 SQLite、immutable Artifact、iteration state；模型会话不具权威性。 |
| 21 | 已接受 | 运行时 Context、持久 Workflow State、长期 Semantic Artifact 三层分离，含精确 iteration/stage 状态枚举。 |
| 22 | 已接受 | 每次模型调用由 Context Builder 按 Skill/任务/Policy/相关 Artifact/Input/Schema 重建有界上下文。 |
| 23 | 已接受 | 实现 provider-neutral Generic Skill Runtime，不依赖 Codex/供应商原生 Skill，不支持动态 Go plugin。 |
| 24 | 已接受 | 13 个 Skill 均要求 `SKILL.md`、input/output Schema 和固定章节；examples 作为可选目录；缺失/重复/未知 manifest ID 失败。 |
| 25 | 已接受 | Runtime 自行读取 Skill/Schema/Context、拼装消息、调用 OpenAI-compatible client、校验并保存；provider native 只能是未来 adapter。 |
| 26 | 已接受 | Skill 定义“怎么做”，Context 定义“当前工程语义”，Input 定义“本批数据”；三者独立哈希和持久引用。 |

## 编号 27-38：输入与分层分析

| # | 状态 | 实际落实 |
|---:|---|---|
| 27 | 已接受 | 关闭但覆盖全部列举 source type，并允许 `custom`；不是固定 disagreement.csv。 |
| 28 | 已接受 | CSV/JSONL/Markdown 经显式 Adapter 转 Canonical Audit Record；V1 拒绝任意二进制格式。 |
| 29 | 已接受 | 每条记录强制 source/selection/comparison/actor/authority/policy provenance；candidate 判断不得冒充 Gold。 |
| 30 | 已接受 | Audit Package 含严格 manifest、多 source、Policy version、objective、permissions、mapping、Change Requests 和 hash/count。 |
| 31 | 已接受 | 未知/custom 先由 `audit-source-interpreter` 生成候选 Mapping 报告；增加人工批准与 source-hash 绑定后才允许 normalize。 |
| 32 | 已接受 | 明令禁止一次把 2,000-10,000 原始记录送模型；Context overflow 只允许确定性拆批，不静默截断。 |
| 33 | 已接受 | 确定性按 scene/source/risk/change/disagreement/Expert 预分层，默认且可配置 70/20/10，总和必须 100。 |
| 34 | 已接受 | Local Worker 每批 30-100、默认 50，只产 candidate pattern/root cause/coverage/representative/counterexample，不改 Policy。 |
| 35 | 已接受 | Global Merge 读取 Local Artifacts；模型提议 merge，代码重算 coverage/batch/source/model/history。 |
| 36 | 已接受 | 每个 Global Pattern 附 3-10 代表、2-5 确定性随机、最多 10 边界/反例并验证真实 record refs。 |
| 37 | 已接受 | Diagnosis 每次 5-20 Global Patterns，原因使用固定 12 类并要求 Artifact evidence 与 alternative explanations。 |
| 38 | 已接受 | 批量用于模式与统计，单条 adjudication 用于 borderline、人工分歧、Policy 关键例和 Critic 反例。 |

## 编号 39-52：变更、编译、回归与发布

| # | 状态 | 实际落实 |
|---:|---|---|
| 39 | 已接受 | Rule Author 只生成含 count/hash/diff/risk/contracts/non-goals 的 Proposal，禁止改生产文件。 |
| 40 | 已接受 | 独立 Critic 与 author/resolver 不同 family，返回 accept/revise/block；accept 不是人工批准。 |
| 41 | 已接受 | Change Request 是独立一等输入，可与数据 Proposal 合并，也可在 direct compile 单独使用。 |
| 42 | 已接受 | 固定 Change Request version/id/source/actor/authority/targets/requested changes/rationale/evidence/time Schema。 |
| 43 | 已接受 | 固定 authority 顺序；高权威不能静默覆盖，同级高权威冲突或不可实现/矛盾/Schema 违规时阻塞并引用原因。 |
| 44 | 已接受 | 正式支持 A 完整分析、B 数据+建议、C Change Request 直接编译三种模式，并锁定各自 stage graph。 |
| 45 | 已接受 | Preview 可停在候选；任何 release 都必须 fresh regression，不存在默认跳过。 |
| 46 | 已接受 | Change Resolver 决定/规范化可采纳 Change Set；Prompt Compiler 只编译已接受 source policy。 |
| 47 | 已接受 | Prompt 是 Common Policy、Cards、Case Type、Evidence Ownership、Role、Schema、Approved Examples 的发布产物。 |
| 48 | 部分接受 | 接受组织、转换、去重和预算控制；拒绝 Compiler 自由语义压缩。任何模型压缩措辞必须先作为 Proposal/Change Request 进入 Policy。 |
| 49 | 已接受 | base/candidate 使用同一冻结 harness，独立报告 fixed/new wrong、双向标签变化、quarantine、rule/category/stage/fallback/contract。 |
| 50 | 已接受 | Precision-first Gate 固定 Core Safe 和 Hard Negative false Unsafe = 0，hidden Safe/Hard Negative 同样零容忍。 |
| 51 | 已接受 | released/candidate 版本目录不可覆盖、删除、复用；候选绑定唯一 base 和 Change Set，发布为原子新目录。 |
| 52 | 已接受 | 自动化止于分析/候选/编译/回归；Mapping、Gold、Policy Directive、Release Approval 保留给人，Approval 绑定 hashes。 |

## 最终原则 1-20

20 条“不可违反的设计原则”全部接受，并分别固化在两份 Design 的角色权限、证据归属、状态/Artifact、Skill、分层分析、Change Request、Compiler、Regression 与 Human Approval 条款中。Harness 将这些原则转成 anti-shortcut、mutation、failure-injection 与 real-acceptance 门禁；不存在仅靠文档声明而无测试任务的原则。

后续 Implementation Contract Freeze 进一步固定了 CLI、版本命名、状态转换、Artifact 路径与敏感等级、11 表 DDL、外部 Schema 字段、确定性 ID/分层算法、13 个 Skill 的逐字指令、Context/Prompt 编译顺序、失败决策和验收 fixture。外部代码模型只保留私有辅助函数与等价标准库算法的局部实现选择。

## 要求输出 1-22 对照

| 输出要求 | 落点 |
|---:|---|
| 1-4 | Safety Review Design 2、4、15；Safety Plan feat-019 至 feat-021。 |
| 5-8 | Safety Review Design 2、3、6、13；Safety Plan feat-016、feat-024。 |
| 9-13 | Policy Optimization Design 1-10；Optimizer Plan feat-026 至 feat-032。 |
| 14-17 | Policy Optimization Design 11-14；Optimizer Plan feat-033 至 feat-035。 |
| 18-19 | Policy Optimization Design 14-15；Optimizer Plan feat-035、feat-037。 |
| 20 | 修订后的 Safety Review Implementation Plan feat-015 至 feat-025。 |
| 21 | 新增 Policy Optimization Implementation Plan feat-026 至 feat-038，并由 Implementation Contract Freeze 固定实现细节。 |
| 22 | 本矩阵；52 项逐项标记，另对 20 条最终原则和 22 个交付项给出落点。 |
