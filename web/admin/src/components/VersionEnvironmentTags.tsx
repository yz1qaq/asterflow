import { Space, Tag, Typography } from 'antd'

type EnvironmentBinding = { env: string; version_id: string }

export function VersionEnvironmentTags({ version, versionID, bindings = [] }: {
  version: number
  versionID: string
  bindings?: EnvironmentBinding[]
}) {
  return <Space size={[8, 4]} wrap>
    <Typography.Text strong>v{version}</Typography.Text>
    {bindings.filter(binding => binding.version_id === versionID).map(binding =>
      <Tag className="version-env-tag" key={binding.env} bordered={false}>{binding.env}</Tag>,
    )}
  </Space>
}
