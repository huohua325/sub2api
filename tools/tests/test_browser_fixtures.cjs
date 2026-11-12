// Run real fixture decisions without a browser dependency.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const fixtureFile = path.join(__dirname, '../browser_fixtures.cjs');
assert.ok(fs.existsSync(fixtureFile), 'Reusable browser fixtures are not implemented');
const makeFixtures = vm.runInNewContext('(' + fs.readFileSync(fixtureFile, 'utf8') + ')');
const fixture = makeFixtures('http://127.0.0.1:4199');
assert.equal(fixture.resolve('/api/v1/settings/public', 'GET').api_base_url, 'http://127.0.0.1:4199');
assert.equal(fixture.resolve('/api/v1/auth/me', 'GET').role, 'user');
assert.throws(() => fixture.resolve('/api/v1/admin/usage-monitor/codex-resets', 'POST'), /Blocked/);
assert.throws(() => fixture.resolve('/v1/responses', 'POST'), /Blocked/);
assert.equal(fixture.resolve('/api/v1/usage/dashboard/api-keys-usage', 'POST').stats.constructor.name, 'Object');
console.log('5 browser fixture contracts passed');
