# Product

## Register

product

## Users

全局管理员、空间管理者、Agent设计者与业务用户。角色与场景来自docs/product/PRD.md；具体可用功能以docs/delivery/STATUS.md为准。

## Product Purpose

AsterFlow提供按空间授权的Agent配置与运行管理。当前按已实现接口逐步增加React界面，与真实Go/PostgreSQL联调。

## Brand Personality

沿用用户已确认的蓝白工作台和顶部导航：清晰、柔和、一致。复用现有Ant Design控件，不另做视觉重设计。

## Anti-references

不使用营销落地页布局、假业务数据或未实现的可点击功能。旧项目的完成情况不代表本项目交付。

## Design Principles

- 当前空间和操作权限清晰可见，后端执行最终授权。
- 一个功能贯通页面、HTTP与数据库后再扩展。
- 操作提供加载、失败重试与成功反馈；身份切换不显示旧结果。
- 凭据只在输入时使用，不回显、不持久化到浏览器。

## Accessibility & Inclusion

沿用有标签的Ant Design表单、键盘焦点和文字状态；保留减少动态效果的现有样式。按用户要求只做桌面联调，不主动增加手机适配或宣称无障碍认证。
