// Run real fixture decisions without a browser dependency.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const fixtureFile = path.join(__dirname, '../browser_fixtures.cjs');
assert.ok(fs.existsSync(fixtureFile), 'Reusable browser fixtures are not implemented');
const browserState = {
  window: {}, location: {hash:'#guide-test'}, scrollY:887, innerHeight:1000,
  absoluteTop:1000,
  document: {documentElement:{scrollHeight:3000}, getElementById:()=>({getClientRects:()=>[{}],getBoundingClientRect:()=>({top:browserState.absoluteTop-browserState.scrollY,bottom:browserState.absoluteTop-browserState.scrollY+40})})},
  performance: {now:()=>browserState.now}, now:0,
};
const makeFixtures = vm.runInNewContext('(' + fs.readFileSync(fixtureFile, 'utf8') + ')',browserState);
const fixture = makeFixtures('http://127.0.0.1:4199');
assert.equal(fixture.resolve('/api/v1/settings/public', 'GET').api_base_url, 'http://127.0.0.1:4199');
assert.equal(fixture.resolve('/api/v1/auth/me', 'GET').role, 'user');
assert.throws(() => fixture.resolve('/api/v1/admin/usage-monitor/codex-resets', 'POST'), /Blocked/);
assert.throws(() => fixture.resolve('/v1/responses', 'POST'), /Blocked/);
assert.equal(fixture.resolve('/api/v1/usage/dashboard/api-keys-usage', 'POST').stats.constructor.name, 'Object');
assert.equal(typeof fixture.alignment, 'function', 'Missing stable-scroll readiness predicate');
assert.equal(fixture.alignment('guide-test'), false, 'A moving scroll passing near its target is not finished');
browserState.now=50; browserState.scrollY=888;
assert.equal(fixture.alignment('guide-test'), false);
browserState.now=200;
assert.equal(fixture.alignment('guide-test'), true);
browserState.absoluteTop=2000;browserState.document.documentElement.scrollHeight=2200;browserState.scrollY=1129;
browserState.now=250;assert.equal(fixture.alignment('guide-test'),false);
browserState.now=400;assert.equal(fixture.alignment('guide-test'),true,'Short final chapter heading is visible without pixel-perfect bottom alignment');
browserState.scrollY=999;browserState.now=500;fixture.alignment('guide-test');
browserState.now=700;assert.equal(fixture.alignment('guide-test'),false,'Off-screen chapter heading must still fail');
console.log('Browser fixture contracts passed');
