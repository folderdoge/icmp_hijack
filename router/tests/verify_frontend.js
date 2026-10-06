#!/usr/bin/env node
'use strict';

// Run: node router/tests/verify_frontend.js
// Execute the shipped page with isolated DOM, AJAX, and timer boundaries.
// No router, browser, network connection, or npm dependency is required.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const page = fs.readFileSync(path.join(__dirname, '..', 'webs', 'Module_icmphijack.asp'), 'utf8');
const inlineScripts = Array.from(page.matchAll(/<script(?:\s[^>]*)?>([\s\S]*?)<\/script>/g),
    match => match[1]).filter(Boolean);
assert.ok(inlineScripts.length, 'page must contain its client logic');
assert.doesNotMatch(page, /查看日志|连接日志|control\.log|daemon\.log|<textarea|\/_temp\//i,
    'the page must expose current status without a log or history panel');

function sandbox() {
    const elements = {};
    for (const tag of page.matchAll(/<([a-z][a-z0-9-]*)([^>]*)>/gi)) {
        const attributes = tag[2];
        const id = attributes.match(/\bid="([^"]+)"/);
        if (!id) continue;
        assert.ok(!elements[id[1]], `duplicate DOM id: ${id[1]}`);
        const value = attributes.match(/\bvalue="([^"]*)"/);
        const type = attributes.match(/\btype="([^"]*)"/);
        elements[id[1]] = {
            id: id[1], value: value ? value[1] : '', type: type ? type[1] : '',
            checked: /\bchecked(?:=|\s|$)/i.test(attributes),
            disabled: /\bdisabled(?:=|\s|$)/i.test(attributes),
            readOnly: /\breadonly(?:=|\s|$)/i.test(attributes),
            style: {}, textContent: '', className: '', attributes: {},
            setAttribute(name, content) { this.attributes[name] = content; },
        };
    }
    const requests = [];
    const timers = new Map();
    let nextTimer = 0;
    const context = {
        document: {
            getElementById(id) {
                assert.ok(elements[id], `missing DOM element: ${id}`);
                return elements[id];
            },
        },
        $: {ajax(request) { requests.push(request); }},
        tabtitle: [[]], tablink: [[]], show_menu(callback) { callback(); },
        setTimeout(callback, delay) {
            const id = ++nextTimer;
            timers.set(id, {callback, delay});
            return id;
        },
        clearTimeout(id) { timers.delete(id); },
    };
    vm.createContext(context);
    for (const script of inlineScripts) vm.runInContext(script, context, {filename:'Module_icmphijack.asp'});
    function nextRequest(expectedUrl) {
        assert.ok(requests.length, `expected request: ${expectedUrl}`);
        const request = requests.shift();
        assert.equal(request.url, expectedUrl);
        return request;
    }
    function complete(request) { if (request.complete) request.complete(); }
    function respond(request, config) {
        request.success({result:[config]});
        complete(request);
    }
    return {context, elements, requests, timers, nextRequest, complete, respond};
}

const config = {
    icmp_hijack_enable:'1', icmp_hijack_server:'14.137.20.5', icmp_hijack_port:'39070',
    icmp_hijack_key:'f'.repeat(64), icmp_hijack_state:'connected',
    icmp_hijack_status:'隧道已连接', icmp_hijack_status_time:'2026-10-06T09:00:00Z',
    icmp_hijack_version:'1.0.7',
};
const test = sandbox();
const {context, elements, requests, timers} = test;
context.init();
test.respond(test.nextRequest('/_api/icmp_hijack_'), config);
assert.equal(elements.server.value, config.icmp_hijack_server);
assert.equal(elements.port.value, config.icmp_hijack_port);
assert.equal(elements.key.value, config.icmp_hijack_key);
assert.equal(elements.key.type, 'password', 'the secret must start hidden');
assert.equal(elements.key.readOnly, false);
assert.equal(elements.enable.checked, true);
assert.equal(elements.enable.disabled, false);
assert.equal(elements.save.disabled, false);
assert.equal(elements.version.textContent, '当前版本：1.0.7');
assert.deepEqual(Array.from(context.tablink[0]), ['', 'Module_icmphijack.asp']);
assert.equal(timers.size, 1);
assert.equal(Array.from(timers.values())[0].delay, 3000);

const states = [
    ['disabled', '已关闭', 'disabled'], ['connecting', '正在连接', 'connecting'],
    ['connected', '已连接', 'connected'], ['connect_error', '连接失败', 'error'],
    ['auth_error', '认证失败', 'error'], ['error', '运行异常', 'error'],
];
for (const [state, label, color] of states) {
    context.renderStatus({...config, icmp_hijack_state:state, icmp_hijack_status:'<img src=x onerror=alert(1)>'});
    assert.equal(elements.state.textContent, label);
    assert.equal(elements.state.className, `icmp-state icmp-state-${color}`);
    assert.equal(elements.status.textContent, '<img src=x onerror=alert(1)>', 'server text must remain text');
}
assert.match(elements.status_time.textContent, /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
assert.equal(context.formatStatusTime(''), '—');
assert.equal(context.formatStatusTime('1770000000'), context.formatStatusTime('1770000000000'));

// Editing a field must survive every status poll, including a failed poll.
elements.server.value = '203.0.113.9';
elements.port.value = '45001';
elements.key.value = 'e'.repeat(64);
elements.enable.checked = false;
function assertEditsPreserved() {
    assert.equal(elements.server.value, '203.0.113.9');
    assert.equal(elements.port.value, '45001');
    assert.equal(elements.key.value, 'e'.repeat(64));
    assert.equal(elements.enable.checked, false);
}
context.refreshStatus();
context.refreshStatus();
assert.equal(requests.length, 1, 'overlapping polls must not create overlapping requests');
test.respond(test.nextRequest('/_api/icmp_hijack_'), {...config, icmp_hijack_state:'auth_error', icmp_hijack_status:'认证失败'});
assertEditsPreserved();
assert.equal(elements.state.textContent, '认证失败');
assert.equal(timers.size, 1, 'refreshing replaces the timer instead of accumulating timers');
context.refreshStatus();
const failedPoll = test.nextRequest('/_api/icmp_hijack_');
failedPoll.error(); test.complete(failedPoll);
assertEditsPreserved();
assert.equal(elements.state.textContent, '运行异常');
assert.match(elements.status.textContent, /读取失败/);

context.selectTab('help');
assert.equal(elements.tablet_1.style.display, 'none');
assert.equal(elements.tablet_2.style.display, '');
assert.equal(elements.apply_button.style.display, 'none');
assert.equal(elements.show_btn2.attributes['aria-selected'], 'true');
context.selectTab('config');
assert.equal(elements.tablet_1.style.display, '');
assert.equal(elements.tablet_2.style.display, 'none');
assert.equal(elements.apply_button.style.display, '');
elements.show_key.checked = true; context.toggleKey(); assert.equal(elements.key.type, 'text');
elements.show_key.checked = false; context.toggleKey(); assert.equal(elements.key.type, 'password');

// Submit through the existing Software Center callback with the edited values.
elements.enable.checked = true;
context.save();
const submitted = test.nextRequest('/_api/');
assert.equal(submitted.type, 'POST');
assert.equal(elements.save.disabled, true);
const payload = JSON.parse(submitted.data);
assert.equal(payload.method, 'icmp_hijack_config.sh');
assert.deepEqual(payload.params, ['apply']);
assert.deepEqual(payload.fields, {
    icmp_hijack_enable:'1', icmp_hijack_server:'203.0.113.9',
    icmp_hijack_port:'45001', icmp_hijack_key:'e'.repeat(64),
});
submitted.success({result:payload.id}); test.complete(submitted);
assert.equal(elements.save.disabled, false);
test.respond(test.nextRequest('/_api/icmp_hijack_'), config);
assert.equal(elements.server.value, '203.0.113.9');
assert.equal(elements.state.textContent, '已连接');

// Validation errors belong to the form; they must not replace daemon status.
for (const invalid of [
    {server:'014.137.20.5', port:'45001', key:'e'.repeat(64), message:/IPv4/},
    {server:'256.1.2.3', port:'45001', key:'e'.repeat(64), message:/IPv4/},
    {server:'203.0.113.9', port:'0', key:'e'.repeat(64), message:/端口/},
    {server:'203.0.113.9', port:'65536', key:'e'.repeat(64), message:/端口/},
    {server:'203.0.113.9', port:'45001', key:'bad', message:/密钥/},
]) {
    elements.server.value = invalid.server; elements.port.value = invalid.port; elements.key.value = invalid.key;
    context.save();
    assert.equal(requests.length, 0, 'invalid configuration must not be submitted');
    assert.match(elements.form_message.textContent, invalid.message);
    assert.equal(elements.state.textContent, '已连接');
    assert.equal(elements.status.textContent, config.icmp_hijack_status);
}

// Turning the feature off must remain possible even with incomplete settings.
elements.enable.checked = false;
elements.server.value = ''; elements.port.value = ''; elements.key.value = '';
context.save();
const disable = test.nextRequest('/_api/');
assert.deepEqual(JSON.parse(disable.data).fields, {
    icmp_hijack_enable:'0', icmp_hijack_server:'', icmp_hijack_port:'', icmp_hijack_key:'',
});
disable.error(); test.complete(disable);
assert.equal(elements.save.disabled, false);
assert.match(elements.form_message.textContent, /提交失败/);
context.stopPolling();
assert.equal(timers.size, 0);
context.refreshStatus();
assert.equal(requests.length, 0);

const unavailable = sandbox();
unavailable.context.init();
const failedInit = unavailable.nextRequest('/_api/icmp_hijack_');
failedInit.error(); unavailable.complete(failedInit);
assert.equal(unavailable.elements.save.disabled, true, 'do not submit over unknown stored configuration');
unavailable.context.save();
assert.equal(unavailable.requests.length, 0);
assert.match(unavailable.elements.form_message.textContent, /读取配置失败/);
unavailable.context.stopPolling();

console.log('PASS: current-state mapping; polls preserve edits; config RPC; hidden key; native tabs; validation; no log/history UI');
