import codingQuota from './codingQuota'
import adminAccessKeys from './adminAccessKeys'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import guide from './guide'
import admin from './admin'
import misc from './misc'
import design from './design'

export default {
  codingQuota,
  adminAccessKeys,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  ...guide,
  admin,
  ...misc,
  ...design,
}
