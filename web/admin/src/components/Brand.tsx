import { DeploymentUnitOutlined } from '@ant-design/icons'

export function Brand() {
  return <span className="brand-lockup">
    <span className="brand-emblem" aria-hidden="true"><DeploymentUnitOutlined /></span>
    <span className="brand-wordmark">AsterFlow</span>
    <span className="brand-edition">星流</span>
  </span>
}
