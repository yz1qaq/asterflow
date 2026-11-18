# G03 验证记录

日期2026-11-17。G03尚未整体完成，无PR/CI/模型调用验收结论。

负责人已保存crypto.go、types.go及迁移000003 up/down，并生成models.go。助手创建crypto_test.go、schema_test.go。
正式目录加载显式测试URL后运行go test -race -count=1 ./internal/platform/modelconfig，通过（2.109s）。
覆盖加密往返、随机nonce、错误密钥/篡改/过短数据拒绝、内部密文不进入JSON；真实隔离PG验证密文BYTEA、账户重名、空间外键、跨空间引用、负价格/零超时/负重试拒绝，down保留原空间数据，再up成功。
测试仅清理随机schema，没有应用开发库迁移。

固定参考映射：类型从service.go拆出，内部密文增加json:"-"；本地000003合并providers基础、账户/部署和计价子集。复合外键限制同空间引用，默认超时120000与Service一致。省略无实际运行时读取方的credential_ref旧占位列，账户路径使用api_key_ciphertext。

下一批8个账户/部署查询仅在临时模块通过sqlc/sqlc:1.27.0生成。正式查询已保存，Store/Service/HTTP/Web尚未接入。

## 账户Store候选

负责人已保存modelconfig.sql及生成文件。账户PostgresStore候选在临时目录使用这些正式生成代码通过真实隔离PG/race验证（2.187s）；不代表正式Store已经保存。候选覆盖创建字段写回、查询/空间列表、密文轮换、重名与主键冲突区分、缺失/非法ID和取消；开发库仍未应用迁移。

## 正式账户Store与部署候选

负责人已保存postgres_store.go；助手接入postgres_store_test.go。正式目录go test -race -count=1 ./internal/platform/modelconfig通过（2.259s），包含真实隔离PG的账户创建/查询/轮换、空间列表隔离、错误映射和取消。

部署方法按职责放入同包deployment_store.go，仍为同一个PostgresStore的方法。候选仅在临时模块验证（2.114s），尚未保存为正式生产文件。覆盖三类价格往返与更新、重名/主键冲突区分、跨空间账户外键拒绝、缺失/非法ID、取消、NaN/Inf/负价格拒绝和int32范围。临时测试复用正式生成代码与账户Store；未应用开发库迁移，无HTTP/Web或真实模型验收结论。

## 正式部署Store与账户Service候选

负责人保存deployment_store.go，助手将部署回归并入postgres_store_test.go。正式目录go test -race -count=1 ./internal/platform/modelconfig通过（2.364s），覆盖账户及部署真实隔离PG行为，开发库迁移仍未应用。

账户Service候选在临时目录通过同命令（1.941s）：加密入库与解密比对、公开响应不含密钥/密文、空输入与URL格式拒绝、重名错误保留、空间列表隔离、跨空间轮换拒绝、正确轮换、服务端身份字段不接受JSON绑定。Service尚未保存于正式目录，无HTTP/Web或模型调用验收。

组织/边界：Service定义消费的Store接口，先列已实现8个方法，后续随Profile等扩展；持久化时间由PG写回。创建请求的workspace_id/created_by不从JSON接受，Handler后续从可信上下文赋值；角色权限由Gin中间件验证，Service记录归属检查不替代成员权限。当前URL检查仅为绝对HTTP(S)格式，固定参考可配置外呼策略及调用前复检在G06实际网关接入时补齐，不用格式校验替代地址访问控制。

## 正式账户Service与部署Service候选

负责人已保存service.go，且删除ListProviderAccounts重复计算HasAPIKey的语句；HasAPIKey由Store映射统一生成。助手接入正式service_test.go；正式目录go test -race -count=1 ./internal/platform/modelconfig通过（2.452s）。

部署Service候选放在同包deployment_service.go，仍为同一个Service的方法，临时模块真实隔离PG/race通过（2.403s）。覆盖创建/列表/价格更新、默认超时与负重试归零、三种价格与int32边界、跨空间拒绝、缺失记录、取消和重名错误保留。候选尚未保存为正式实现。

保持固定参考的TimeoutMS<=0使用120000、MaxRetries<0归零语义。Service提前以ErrInvalidArgument报告无效数值，Store继续保证数值编码/转换安全。价格更新后重查数据库以返回当前持久化字段和真实updated_at，替代参考在Service用time.Now估算时间。空间与创建者仍由可信上下文注入。无开发库迁移应用、HTTP/Web或模型调用验收。

## 正式部署Service与加密配置准备

负责人保存deployment_service.go；助手把部署Service测试并入service_test.go。正式归组go test -race -count=1 ./internal/platform/modelconfig通过（2.374s）。

加密配置候选在临时模块internal/config验证通过（1.580s）：缺失/空/短/全空白主密钥拒绝、错误不回显密钥、合法值原样保留及既有配置测试。正式config.go尚未修改，正式配置测试待负责人保存后同步接入。

运维已确认.env被Git忽略，生成独立32随机字节的十六进制主密钥，保留在.env并设置0600；未回显、未提交，也未改JWT密钥。.env.example只增加空配置项。主密钥不是供应商API Key，必须跨重启保持一致；服务启动装配和HTTP仍待后续。

开发库make migrate-version本轮实查version=2 dirty=false。迁移000003尚未应用；下一步由负责人执行up，再以版本/实际表检查为证，不能把隔离测试当作开发库已迁移。

## 正式配置与开发库迁移、账户HTTP候选

负责人保存CredentialSecretKey读取与校验；助手接入正式config_test.go，正式config包race通过（1.508s）。进程测试环境另设独立凭据加密夹具并过滤真实主密钥，不从本地环境继承生产加密配置。

开发库make migrate-version实查version=3 dirty=false；另以容器psql只读确认public.schema_migrations为3/false，public.providers与public.model_deployments存在。迁移由负责人执行，助手未代执行开发库up/down。

账户HTTP、NewRouter新增模型Service参数、main装配候选仅位于临时完整Go目录。临时go test -race -count=1 ./internal/api/... ./cmd/server通过：api 24.959s，middleware 2.657s，v1 2.985s，server 10.145s。包括真实隔离PG/Auth/Workspace链的创建201、列表、重名409、轮换204空体、缺token401、缺空间400、viewer写403、跨空间轮换404、非法ID/请求体/身份字段注入400及密文解密核对。旧路由及进程启停回归在临时目录更新构造调用后通过，不代表正式HTTP代码已保存。

保持固定参考/model-accounts及PUT /model-accounts/:account_id/api-key路径，Gin适配返回统一JSON错误；数据库内部错误仅返回通用500。后续接部署HTTP和增量Models界面，再推进Profile。

## 正式账户HTTP与账户Web闭环

负责人保存modelconfig Handler、三参数NewRouter及main模型Service装配。助手同步旧路由测试并接入modelconfig_test.go。正式go test -race -count=1 ./internal/api/... ./cmd/server通过：api 23.511s、middleware 1.713s、v1 4.423s、server 11.702s。测试隔离PG验证认证/空间/角色/创建/重名/轮换/拒绝与数据库密文，不访问模型供应商。

助手增量复用固定参考ModelsPage账户表格、创建/轮换流程，新增model.ts、ModelsPage和导航，沿用现有React/AntD蓝白工作台。只展示账户功能，尚无部署/Profile/绑定菜单。显式空间Header与响应空间核对，用户/空间变化取消旧读取、清空表单；权限从当前context重新确认。密码仅本次提交使用，写操作不用React Query mutation以免凭据留在其variables缓存，成功/取消清空密码，失败保留填写。浏览器角色控制只辅助交互，Go中间件执行授权。

make web-check通过：42项单测（新增8项账户API契约测试），TypeScript与Vite构建通过。共享client块562.50kB仍为已知非阻断提示。

1440x1000和1280x900真实桌面浏览器：从登录、创建/选择空间进入模型页；创建201、同名409且保留输入、改名重试201、轮换204、刷新持久读取、空间B空列表、切回A恢复、切空间后密码清空；viewer只读且无创建/轮换按钮。无页面水平溢出；当前页面控制台无警告或错误。localStorage未发现测试API Key。未做手机测试、付费模型调用或生产部署。

浏览器使用独立随机schema、18091/15175测试进程与独立测试密钥，未向开发public写入测试记录；测试后已退出登录、停止仅测试进程、删除精确随机schema和临时密钥文件。截图仅在本机临时目录，未入库。开发库维持3/false及两张模型表。Web验收不代表G03已完成或已通过远程CI。

## 部署HTTP候选

正式账户HTTP与Web保持上一节已验收状态。下一批部署Handler三个方法及workspaceRoutes注册仅在临时目录预验证，尚未写入正式生产Go。
临时go test -race -count=1 ./internal/api -run '^TestModelAccountsHTTP$'通过（4.970s）。该同组用例覆盖已有账户链及部署创建201/重名409、列表与空间隔离、价格更新200且时间/价格与PG回读一致；缺token401、缺空间400、viewer写403、跨空间引用/更新404、非法ID/负价/JSON身份注入/多JSON拒绝。
接口保持固定参考：POST/GET /model-deployments，PUT /model-deployments/:deployment_id/pricing。Handler复用既有可信context、decodeJSON和writeModelConfigError，main与Service无需改动。当前Models界面仍只有账户功能；部署HTTP保存后再增量接部署界面，无模型外部调用、远程CI或G03整体完成声明。

## 正式部署HTTP与部署Web闭环

负责人已保存三个部署Handler和workspaceRoutes注册。助手把部署用例并入正式modelconfig_test.go并命名TestModelConfigHTTP；正式go test -race -count=1 ./internal/api -run '^TestModelConfigHTTP$'通过（5.119s）。覆盖账户/部署真实PG持久化、创建/重名/列表/三项价格更新、可信身份、跨空间/角色/输入拒绝。

Models页面增量加入供应商账户/模型部署页签；新建ModelDeploymentsPanel复用当前身份和空间验证、现有AntD与蓝白布局。只加载当前空间账户，账户为空时解释创建前置条件并提供跳转。表单回填超时120000ms、重试0和三项价格；编辑时读取已有三项价格，提交完整计价字段，0可正常保存。普通失败保留输入，成功/取消清空，切换空间或页签卸载旧表单；异步结果先核对当前身份/空间。API显式空间Header、取消读取、响应归属与数值校验、参数白名单和更新目标ID核对。

make web-check通过：50项单测（新增8项部署API契约测试）、TypeScript/Vite构建。共享client 562.60kB仍有非阻断构建提示。

真实Go/随机隔离PG/桌面浏览器1440x1000、1280x900验证：无账户时禁止创建并能前往账户配置；创建部署201，超时/重试默认值正确；价格回填1.25/2.5/0.1，更新为0/3.5/0.05返回200，刷新持久保留；重名409保留输入，改名重试201。空间B无旧部署/账户，切回A恢复；viewer看到2条只读部署，无创建/编辑按钮；页面无水平溢出。控制台仅有故意触发重名409的网络提示，无其他运行时警告。测试未调用外部模型，未主动测试手机。

仅操作独立随机schema与18091/15175测试进程；已退出登录、停止测试进程并清理精确schema和临时凭据。开发public无本轮测试数据；本机截图未入库。账户/部署阶段完成，G03的Profile、版本与环境绑定尚未完成，无PR/CI/完整模块或生产部署结论。

## Profile结构候选

下一批仅为ModelProfile/ModelProfileVersion类型与000004_model_profiles迁移候选，尚未保存为正式生产文件。sqlc/sqlc:1.27.0生成成功，JSONB生成FallbackDeploymentIds []byte；业务DTO继续使用[]string。

临时目录真实随机schema执行go test -race -count=1 ./internal/platform/modelconfig -run '^TestProfileSchemaCandidate$'通过（2.003s）。验证Profile同空间名称唯一/跨空间同名允许、Workspace/Profile/主Deployment外键存在性、版本号正数及同Profile内唯一、不同Profile可各有v1、备选JSON数组默认[]和internal默认值、对象/JSON null/SQL NULL拒绝、主Deployment引用保护、Profile删除级联版本、000004 down保留账户/部署/IAM数据、再up成功。仅清理随机schema，没有应用开发库000004。

固定参考000013的Profile/Version语义保留，补Workspace FK、version>0和JSON数组形状约束。外键保证主记录/主部署存在，不代表已完成同空间校验；备选JSON没有逐元素外键，后续Service逐个核验存在性/空间，Store统一把空备选列表编码为[]。本轮表结构本身不阻止SQL UPDATE，版本不可变通过后续只新增的业务/存储接口落实。版本固定部署引用和分级，部署计价沿用独立更新机制。

后续Profile与首版本使用必需原子事务，不保留可选非事务路径；后续同Profile版本号分配需在事务内串行化，不能用无锁len(versions)+1冒充并发安全。项目环境绑定与限额字段另步接入；账户/部署Web当前仍是唯一模型管理入口，无新Profile菜单或模型调用。

## 正式Profile结构与查询候选

负责人已保存ModelProfile/ModelProfileVersion及000004 up/down。助手将结构测试并入现有schema_test.go（TestModelProfileSchema），正式go test -race -count=1 ./internal/platform/modelconfig -run '^TestModelProfileSchema$'通过（2.228s）。本轮只读make migrate-version实查开发库仍为version=3 dirty=false，000004尚未应用。

下一批8个Profile查询仅临时预验证：Create/List/Get/LockModelProfile、NextModelProfileVersion、Create/List/GetProfileVersion。固定sqlc生成成功；NextModelProfileVersion显式bigint，生成int64。临时真实隔离PG/race的TestProfileQueriesCandidate通过（1.761s），覆盖方案/版本写入与返回、空间过滤、空序列得1、v1后得2、同事务锁后插入v2/提交后得3、版本升序、缺失记录与重名约束名。

查询本身不实现完整业务原子性或并发分配：后续Store须在同一事务内锁Profile行、读取MAX+1、检查int32上限并插入；方案与首版本也必须同事务。该候选测试不等于生产Store的并发验证。正式查询待负责人保存；允许负责人在已保存/正式验证的000004上执行开发库up并提供version证据，助手未代应用迁移。

## 正式Profile查询与原子创建Store候选

负责人已保存8个Profile查询及sqlc生成文件。助手将查询测试并入现有postgres_store_test.go（TestPostgresProfileQueries），正式隔离PG/race通过（1.936s）。本轮make migrate-version实查开发库version=4 dirty=false；容器psql只读确认public.model_profiles与public.model_profile_versions存在。

下一批profile_store.go候选仅在临时目录，尚未保存为正式实现。包含方案/版本记录转换、空备选编码[]以及必需的CreateProfileWithVersion事务。临时隔离PG/race的TestProfileAtomicStoreCandidate通过（1.949s）：成功规范UUID/写回数据库时间和非nil空数组；第二条INSERT的主部署FK失败或版本主键冲突时方案一起回滚；失败不修改调用方对象；同空间重名映射、无效ID/关联/首版号、nil、取消及坏JSON拒绝。

候选先转换版本返回记录，再Commit，最后才写回profile/version；提交前出错走Rollback。不把提交通信错误等同于数据库一定未提交；不提供自动重试创建。初始版本必须1且ProfileID匹配，新建方案无需调用已有方案行锁算号；后续追加版本再使用锁/MAX+1。候选尚无Service/HTTP/Web入口，引用同空间规则在后续Service接入，不以此测试代表G03整体完成。

## 正式原子创建Store与读取/追加候选

负责人已保存profile_store.go。助手将用例并入现有postgres_store_test.go（TestPostgresProfileAtomicCreate），正式隔离PG/race通过（1.754s）。没有替负责人改写生产Go。

下一批读取与追加方法仅在临时副本预验证。TestProfileReadAppendCandidate隔离PG/race通过（2.029s），覆盖方案/版本回读与列表、空列表、缺失/非法ID、v1不变、忽略调用方Version=999而分配v2、插入外键失败不写回/不占号、6个并发请求得到唯一连续版本号、版本升序、观察pg_stat_activity确实进入行锁等待后取消且无遗留版本、int32上限拒绝。

追加路径为同事务LockModelProfile→NextModelProfileVersion→范围检查→CreateProfileVersion→转换→Commit→写回。并发保证依赖所有应用追加路径遵守相同Profile行锁协议；普通SELECT不因此阻塞。Service后续负责当前空间与全部部署引用合法性，Store读取成功不授予访问权限。候选尚未成为正式Service/HTTP/Web能力，无新迁移、模型调用、Git提交或G03完成声明。

## 已保存读取/追加的两处待修正项

负责人已保存Get/List与CreateProfileVersion。正式go test检查失败：profile_store.go:200的fmt.Errorf包含%w但未传err，未执行运行期用例。代码核查同时发现追加末尾*saved = *version方向反转，数据库可能已提交但调用方拿不到真实版本号/时间。

助手将读取/追加用例合并到现有postgres_store_test.go（TestPostgresProfileReadAppend），不另建正式测试文件。在临时副本仅补err后，测试以store did not assign committed version失败；再将赋值改为*version = *saved，隔离PG/race通过（1.750s），包含并发、行锁等待取消、回滚与溢出。正式业务Go未由助手修改，待负责人保存两行修正后复验；本轮未推进Profile Service/API/Web。

## 正式Store修正通过与创建Service候选

负责人已保存两行修正。正式go test -race -count=1 ./internal/platform/modelconfig -run '^TestPostgresProfile(ReadAppend|AtomicCreate)$'通过（2.068s），覆盖原子首版创建、查询及追加并发/取消/回滚/上限。正式业务文件未由助手代写。

下一批仅临时新增profile_service.go与Service消费接口必需CreateProfileWithVersion。CreateProfileRequest将WorkspaceID/CreatedBy标记json:"-"；resolveProfileDeployments统一UUID、读取所有不同部署并检查当前空间，保留备选顺序/重复项且不修改请求切片；CreateProfile组装v1并调用原子Store，数据库唯一约束直接判重，不依赖先列出再检查。

临时TestProfileCreateServiceCandidate先因缺少请求类型/Service方法失败，加入候选后隔离PG/race通过（1.835s）：规范化/默认internal/数据库回读一致、空备选[]、重复名称、缺失及跨空间主/备部署、空/非法ID拒绝且不新增记录、取消原因保留。未调用真实模型。此处不新增分级枚举限制或声称已执行运行期分级策略；Service还没有正式HTTP/Web入口，G03仍在进行中。

## 正式创建Service与查询/追加Service候选

负责人已保存CreateProfileRequest、resolveProfileDeployments、CreateProfile及消费侧必需原子Store接口。助手将创建用例并入现有service_test.go的TestProfileCreateService，正式隔离PG/race通过（1.904s）。

下一批读取/追加Service候选仅在临时副本：GetProfile要求当前Workspace并隐藏跨空间记录；ListProfiles按空间查；ListProfileVersions先检查所属Profile；CreateProfileVersionRequest收拢参数，先检查父方案再检查主备部署，版本号由已有Store事务分配。相对固定参考的len(versions)+1，分配移至加锁Store；相对分散的上层空间检查，读/追加Service显式接收Workspace并复用GetProfile，功能和版本语义保留。

临时TestProfileVersionServiceCandidate先因缺少方法/请求类型失败，实现候选后隔离PG/race通过（1.779s），覆盖空间隔离、UUID规范化、缺失/非法父方案、版本顺序、v1不变、v2/v3成功写回、默认internal、主备部署跨空间/缺失/非法拒绝且无新增版本、请求切片不变及取消原因保留。正式Profile HTTP/Web尚未接通，未作整体G03交付声明或Git操作。

## 正式Profile Service与四个HTTP候选

负责人已保存查询/追加Service和Store消费接口。助手将读取/追加用例并入既有service_test.go的TestProfileVersionService；正式TestProfile(Create|Version)Service隔离PG/race通过（2.216s）。

四个Profile Handler及workspaceRoutes仅在临时副本：POST/GET /model-profiles，POST/GET /model-profiles/:profile_id/versions。创建响应保持固定参考{profile,version}；追加响应单版本。profile_id来自Gin URL，Workspace/CreatedBy来自可信context；统一错误映射补Profile/Version不存在404、Profile同名409，viewer读允许写拒绝。

临时既有TestModelConfigHTTP扩展后PG/race通过（4.258s），保留账户/部署用例，新增Profile/v1创建、空间列表与viewer读取、追加v2及PG回读/版本顺序/v1不变，401/400/403/404/409、路径非法/不存在、JSON身份或版本号注入/多JSON拒绝、跨空间主备部署/父方案拒绝且无额外版本。测试只用随机schema，无外部模型调用。正式Handler/router未由助手代写，HTTP候选不等于正式API或Web已交付；空间全版本列表/环境绑定仍在G03后续范围。

## 正式Profile HTTP与桌面Web闭环（2026-11-18）

负责人保存四个Handler及路由。助手将Profile HTTP用例并入既有internal/api/modelconfig_test.go，正式TestModelConfigHTTP隔离PG/race通过（6.074s），包含账户/部署回归、Profile/v1创建/列表、追加v2、PG回读、v1不变、身份/角色/跨空间/非法输入拒绝。

助手增量添加modelProfiles.ts API适配与ModelProfilesPanel.tsx，Models页新增模型方案页签。创建/追加表单只选择当前空间部署，空部署给出前置提示；方案和版本ID分开显示，备选顺序明确。权限沿用实时context，写请求显式空间Header和字段白名单，响应核对空间及版本所属方案；版本号由服务端返回。写失败保留输入，成功/取消清空，异步结果先核对身份/空间与挂载状态，403刷新权限。旧版本没有编辑入口。

57项Web单测通过（新增7项Profile API用例，仍在既有model.test.ts）；TypeScript/Vite生产构建通过，共享client约562.6kB提示仍非阻断。桌面浏览器1280x720、真实Go/随机隔离PG验证创建方案/v1（Primary、无备选、internal）、追加v2（Fallback、备选Primary）、刷新后重新选择仍保留两版，重名显示提示且保留名称/主部署输入；切至空间B不显示A记录，无部署时新建禁用并提示前置配置。无水平溢出；最终日志无运行时错误，早先测试代理不可用已解决。viewer本轮只由HTTP测试验证，未额外执行viewer浏览器流程。

测试使用18091/15175与独立随机schema。初次后端配置导出错误导致默认端口启动，已停止；自动审批阻止一次重启后，先只读验证导出地址127.0.0.1:18091、随机schema及端口空闲，再加启动断言获准恢复。后续联调全部使用专用端口。浏览器已退出测试账号，仅测试进程已停止，精确随机schema和临时凭据已删除；未修改开发public数据、未调用外部模型、未主动测试手机。截图仅本机临时目录，不入库。G03剩余空间全部版本列表、环境绑定与限额配置待完成，没有Git提交/远程CI/整体交付声明。

## 正式环境绑定 HTTP 与桌面联调（2026-11-18）

负责人补齐 Store 的 UUID 错误检查、绑定 Service、三个正式 Gin Handler 和路由。TestModelConfigHTTP 与 TestPostgresBindingQueries 在隔离 PostgreSQL 开启 race 检查通过，覆盖当前空间全版本列表、Viewer 权限、跨空间版本拒绝、主备计价检查、dev/prod 分离和写入失败保留旧绑定。

React 新增环境绑定页签，只有返回 404 且 code 为 model_binding_not_found 才显示空态。真实桌面浏览器 1280x820 验证 dev 保存 Support v1（预算125.5、RPM 37、TPM 2500）、对未定价备用部署启用预算被拒绝并保留输入、prod 独立保存 v2（RPM 81、预算0），刷新后两个环境独立回读，staging保持未绑定。切换到无方案的空间无旧版本数据、保存入口禁用；Viewer 登录能查看但没有编辑按钮。仅测试随机schema，无真实模型调用。

61项Web测试、TypeScript/Vite构建、Go依赖检查、全量race测试、vet和编译通过。本轮开头的 make check 在 docs-check 阶段发现 binding_store.go:51 行尾空白，待负责人格式化后再次确认全套检查。开发库只读查询为 version=4 dirty=false；待负责人执行000005并验证，尚未提交和合并。

## 提交前集中检查与独立 AI 预审（2026-11-18）

负责人已格式化 Go 文件并应用000005。开发库只读核对为 version=5 dirty=false，已确认 project_model_bindings 主外键和环境、预算、RPM、TPM 约束。SQLC 1.27.0 在临时目录重新生成与工作区逐文件一致。显式测试URL下完整 make check 通过，包含61项Web测试。

独立 AI 按本地工作区、基线 ad533f7 预审。正式 PR 仍未创建，须在修复后复验，并按实际提交 SHA 复审。真实HTTP/隔离PG复现两条后端现象：Base URL 带查询凭据会被Viewer读到；改价 PUT 传 null 会清零，且已有的正预算绑定仍指向该部署。分别修复URL查询参数校验、改价完整请求校验，以及改价与正预算绑定的写入一致性。浏览器已复现后台权限刷新卸载弹窗并丢失草稿；改为仅首次加载使用Skeleton，同一隔离桌面流程确认权限刷新后保留草稿。页面收到权限错误仍显示错误。未发送GitHub Review、未提交和合并。

## 工作区复审通过，准备提交 PR（2026-11-18）

正式模型写入通过同一工作空间行锁协调。绑定在锁内检查主备部署价格；改价在锁内检查当前部署是否被任意正预算环境引用，冲突返回 409 deployment_price_required。新增用例并入现有 HTTP 和 Store 测试文件。真实 PostgreSQL 并发用例在两种顺序里都通过 pg_stat_activity 确认第二笔事务等待锁，随后确认失败方的配置未保存。完整 make check 使用显式测试库配置通过，包含62项Web测试和生产构建；SQLC 1.27.0 在临时目录重新生成后与工作区文件逐字节一致。

桌面 1280x820、真实 Go API 与随机测试 schema：尝试把已被正月预算引用的备用部署改成零价，收到 409 和业务提示，弹窗保留输入。取消后仍显示旧价，环境仍指向原版本并保留预算。仅操作隔离数据，未调用外部模型。

独立 AI 工作区复审确认四项 P2 已关闭，无新阻断。保留 P3 后续项：在之后的迁移补齐价格有限值和直接 SQL 写入的跨空间引用约束；禁止复用 JWT 与凭据主密钥；编辑权限被降级时主动清空打开的编辑表单。当前服务层检查和后端授权已验证，P3 不等同于新活动的越权写入。开发库迁移仍为已实查的5/false，本轮未修改开发库。

[PR 正文](G03-PR.md)已准备。负责人完成提交和推送后核对实际 head SHA、远程 CI 和复审；此工作区结论不是远程 CI 或人类 Approve。
