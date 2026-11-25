import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import audit from './audit'
import promptAudit from './promptAudit'
import plugins from './plugins'
import usageMonitor from './usageMonitor'
import apiFallback from './apiFallback'
import developmentWorkspace from './developmentWorkspace'

export default {
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...promptAudit,
  ...plugins,
  ...usageMonitor,
  ...apiFallback,
  developmentWorkspace,
}
