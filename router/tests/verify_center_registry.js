#!/usr/bin/env node
'use strict';

// Run: node router/tests/verify_center_registry.js
// The fixture follows the actual Software Center registry parser and icon rule:
// https://github.com/koolshare/rogsoft/blob/a05d7b362dec0b663eb1bd94108a0d81778bba60/softcenter/softcenter/webs/Module_Softcenter.asp#L465-L529
// This test needs no router, network connection, npm packages, or browser.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const router = path.resolve(__dirname, '..');
const install = fs.readFileSync(path.join(router, 'install.sh'), 'utf8');
const uninstall = fs.readFileSync(path.join(router, 'uninstall.sh'), 'utf8');

// Preserve upstream's behavior: underscore component 2 is the name, the rest
// form the property, and modules without their own install property disappear.
function formatLocalData(registry) {
    const modules = {};
    for (const [key, value] of Object.entries(registry)) {
        const components = key.split('_');
        if (components[1] !== 'module') continue;
        const name = components[2];
        const property = components.slice(3).join('_');
        if (!modules[name]) modules[name] = {name};
        if (property) modules[name][property] = value;
    }
    for (const name of Object.keys(modules)) {
        if (!modules[name].install) delete modules[name];
    }
    return modules;
}

function installedCards(registry) {
    const modules = formatLocalData(registry);
    for (const card of Object.values(modules)) {
        if (!card.home_url) card.home_url = `Module_${card.name}.asp`;
        // The installed card's icon is derived from name, not a registry field.
        if (parseInt(card.install, 10) !== 0) card.icon = `/res/icon-${card.name}.png`;
    }
    return modules;
}

function registration(name, status = '4') {
    const prefix = `softcenter_module_${name}_`;
    return {
        [`${prefix}name`]: name,
        [`${prefix}title`]: 'ICMP TCP 隧道',
        [`${prefix}description`]: 'LAN ICMP 经 TCP 在远端落地',
        [`${prefix}version`]: 'fixture',
        [`${prefix}install`]: status,
        [`${prefix}home_url`]: `Module_${name}.asp`,
    };
}

// Reproduce the real failure: the old keys never produce an installed card.
assert.deepEqual(installedCards(registration('icmp_hijack', '1')), {});
const fixture = installedCards(registration('icmphijack'));
assert.deepEqual(Object.keys(fixture), ['icmphijack']);
assert.equal(fixture.icmphijack.name, 'icmphijack');
assert.equal(fixture.icmphijack.install, '4');
assert.equal(fixture.icmphijack.home_url, 'Module_icmphijack.asp');
assert.equal(fixture.icmphijack.icon, '/res/icon-icmphijack.png');

// Read the real installer's literal dbus assignments instead of maintaining a
// second expected registration disconnected from the shipped script.
const versionMatch = install.match(/^VERSION=([0-9.]+)\s*$/m);
assert.ok(versionMatch, 'installer must declare its version');
const registry = {};
for (const line of install.split(/\r?\n/)) {
    const match = line.match(/^\s*dbus\s+set\s+(?:"([^"]+)"|'([^']+)'|(\S+))\s*$/);
    if (!match) continue;
    const assignment = match[1] ?? match[2] ?? match[3];
    const separator = assignment.indexOf('=');
    assert.ok(separator > 0, `invalid dbus assignment: ${line}`);
    const key = assignment.slice(0, separator);
    if (!key.startsWith('softcenter_module_')) continue;
    const value = assignment.slice(separator + 1).replace(/\$\{?VERSION\}?/g, versionMatch[1]);
    assert.ok(!value.includes('$'), `unresolved registration variable: ${line}`);
    registry[key] = value;
}
assert.ok(Object.keys(registry).length > 0, 'installer must register a module');
assert.ok(Object.keys(registry).every(key => key.startsWith('softcenter_module_icmphijack_')),
    'software-center names cannot contain underscores');
const actualCards = installedCards(registry);
assert.deepEqual(Object.keys(actualCards), ['icmphijack']);
const card = actualCards.icmphijack;
assert.equal(card.name, 'icmphijack');
assert.equal(card.install, '4', 'offline install must use installed status 4');
assert.equal(card.version, versionMatch[1]);
assert.equal(card.home_url, 'Module_icmphijack.asp');
assert.equal(card.icon, '/res/icon-icmphijack.png');
assert.ok(card.title && card.description);

function copiedSource(destination) {
    for (const line of install.split(/\r?\n/)) {
        const match = line.match(/^\s*cp\s+(?:"([^"]+)"|'([^']+)'|(\S+))\s+(?:"([^"]+)"|'([^']+)'|(\S+))\s*$/);
        if (!match) continue;
        const source = match[1] ?? match[2] ?? match[3];
        const target = match[4] ?? match[5] ?? match[6];
        if (target !== destination) continue;
        assert.ok(source.startsWith('$PACKAGE/'), `unexpected package source: ${source}`);
        const packageSource = path.join(router, source.slice('$PACKAGE/'.length));
        assert.ok(fs.existsSync(packageSource), `missing package source: ${packageSource}`);
        return packageSource;
    }
    assert.fail(`installer does not copy the card's required file: ${destination}`);
}

const pageDestination = `/koolshare/webs/${card.home_url}`;
const iconDestination = `/koolshare${card.icon}`;
const uninstallDestination = '/koolshare/scripts/uninstall_icmphijack.sh';
assert.ok(fs.readFileSync(copiedSource(pageDestination), 'utf8').includes('ICMP'));
assert.equal(fs.readFileSync(copiedSource(iconDestination)).subarray(0, 8).toString('hex'),
    '89504e470d0a1a0a', 'installed icon must be a PNG');
assert.equal(path.basename(copiedSource(uninstallDestination)), 'uninstall.sh');
assert.match(uninstall, /dbus\s+list\s+softcenter_module_icmphijack_/,
    'uninstaller must remove the canonical registration');
for (const target of [pageDestination, iconDestination, uninstallDestination]) {
    assert.ok(uninstall.includes(target), `uninstaller must remove ${target}`);
}

console.log('PASS: old underscore registry loses its card; actual installer creates the canonical card, page, icon, and uninstall entry');
