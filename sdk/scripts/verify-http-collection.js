#!/usr/bin/env node
/*
 * 验证 Postman / Apifox 集合里的签名脚本确实算得出正确签名。
 *
 * 为什么要有这个脚本：
 *   签名错了的表现永远是 401，而 401 和"AK 填错"长得一模一样。手写一个
 *   canonical 拼接、看起来毫无问题、实际少拼一行，这种错误在 review 时
 *   几乎看不出来。所以这里直接**取出集合文件里的那段脚本原样执行**，
 *   喂夹具向量，比对签名 —— 验的是要交付出去的东西，不是复刻一份逻辑。
 *
 * 用法：
 *   node sdk/scripts/verify-http-collection.js
 *
 * 退出码：0 全部通过 / 1 有失败
 */

'use strict';

const fs = require('fs');
const path = require('path');
const vm = require('vm');
const crypto = require('crypto');

const SDK_ROOT = path.dirname(__dirname);
const HTTP_DIR = path.join(SDK_ROOT, 'http');
const COLLECTION = path.join(HTTP_DIR, 'uvp-openapi.postman_collection.json');
const APIFOX = path.join(HTTP_DIR, 'uvp-openapi.apifox.json');
const FIXTURE = path.join(SDK_ROOT, 'fixtures', 'signature-v1.json');

const SIGNATURE_HEADERS = [
  'X-UVP-Sign-Version',
  'X-UVP-Access-Key',
  'X-UVP-Timestamp',
  'X-UVP-Nonce',
  'X-UVP-Signature',
];

let failures = 0;
let checks = 0;

function ok(message) {
  checks += 1;
  console.log('  \u2713 ' + message);
}

function bad(message, detail) {
  checks += 1;
  failures += 1;
  console.log('  \u2717 ' + message);
  if (detail) {
    String(detail).split('\n').forEach(function (line) {
      console.log('      ' + line);
    });
  }
}

function assertEqual(label, actual, expected) {
  if (actual === expected) {
    ok(label);
  } else {
    bad(label + '\n实际: ' + JSON.stringify(actual) + '\n期望: ' + JSON.stringify(expected));
  }
}

function assertThrows(label, fn, needle) {
  try {
    const value = fn();
    bad(label + '（没有报错，返回 ' + JSON.stringify(value) + '）');
  } catch (error) {
    if (needle && String(error.message).indexOf(needle) < 0) {
      bad(label + '（报错信息不含 "' + needle + '"，实际: ' + error.message + '）');
    } else {
      ok(label);
    }
  }
}

/* ---------------------------------------------------------------- CryptoJS mock
 *
 * 只实现集合脚本用到的那几个接口。HMAC / SHA256 交给 node 的 crypto，
 * 所以这里验证的是「脚本拼出来的 canonical 对不对」——
 * 那才是手写最容易错、而且错了还看不出来的部分。
 */

const ENC_HEX = { name: 'Hex' };
const ENC_BASE64 = { name: 'Base64' };
const ENC_UTF8 = { name: 'Utf8' };

class WordArray {
  constructor(buffer) {
    this.buffer = buffer;
  }
  toString(enc) {
    if (!enc || enc === ENC_HEX) {
      return this.buffer.toString('hex');
    }
    if (enc === ENC_BASE64) {
      return this.buffer.toString('base64');
    }
    if (enc === ENC_UTF8) {
      return this.buffer.toString('utf8');
    }
    return this.buffer.toString('hex');
  }
}

function toBuffer(value) {
  if (value instanceof WordArray) {
    return value.buffer;
  }
  return Buffer.from(String(value), 'utf8');
}

function makeCryptoJS(randomQueue) {
  return {
    enc: {
      Hex: Object.assign({}, ENC_HEX, {
        parse: function (text) { return new WordArray(Buffer.from(String(text), 'hex')); },
      }),
      Base64: Object.assign({}, ENC_BASE64, {
        parse: function (text) { return new WordArray(Buffer.from(String(text), 'base64')); },
      }),
      Utf8: Object.assign({}, ENC_UTF8, {
        parse: function (text) { return new WordArray(Buffer.from(String(text), 'utf8')); },
      }),
    },
    SHA256: function (input) {
      return new WordArray(crypto.createHash('sha256').update(toBuffer(input)).digest());
    },
    HmacSHA256: function (message, key) {
      return new WordArray(
        crypto.createHmac('sha256', toBuffer(key)).update(toBuffer(message)).digest()
      );
    },
    lib: {
      WordArray: {
        random: function (byteCount) {
          if (randomQueue.length) {
            return new WordArray(randomQueue.shift());
          }
          return new WordArray(crypto.randomBytes(byteCount));
        },
      },
    },
  };
}

/* ---------------------------------------------------------------- pm mock */

function makePm(options) {
  const variables = new Map(Object.entries(options.vars || {}));
  const headers = (options.headers || []).map(function (item) {
    return { key: item.key, value: item.value };
  });
  let pendingUrl = null;

  const pm = {
    variables: {
      get: function (key) { return variables.has(key) ? variables.get(key) : undefined; },
      set: function (key, value) { variables.set(key, value); },
    },
    request: {
      method: options.method,
      url: {
        path: options.pathSegments,
        getPath: function () {
          if (options.path === null) {
            return '';
          }
          return options.path;
        },
        getQueryString: function () {
          return options.query === undefined ? '' : options.query;
        },
        update: function (value) { pendingUrl = value; },
        toString: function () { return options.rawUrl || ''; },
      },
      headers: {
        get: function (key) {
          const found = headers.filter(function (item) {
            return item.key.toLowerCase() === String(key).toLowerCase();
          });
          return found.length ? found[0].value : null;
        },
        upsert: function (item) {
          const existing = headers.filter(function (h) {
            return h.key.toLowerCase() === String(item.key).toLowerCase();
          })[0];
          if (existing) {
            existing.value = item.value;
          } else {
            headers.push({ key: item.key, value: item.value });
          }
        },
        remove: function (key) {
          for (let i = headers.length - 1; i >= 0; i -= 1) {
            if (headers[i].key.toLowerCase() === String(key).toLowerCase()) {
              headers.splice(i, 1);
            }
          }
        },
      },
      body: options.body === undefined ? undefined : { mode: 'raw', raw: options.body },
    },
  };

  return {
    pm: pm,
    header: function (key) {
      const found = headers.filter(function (item) {
        return item.key.toLowerCase() === String(key).toLowerCase();
      })[0];
      return found ? found.value : null;
    },
    canonical: function () { return variables.get('uvp_canonical'); },
    signatureFromScript: function () { return variables.get('uvp_signature'); },
    updatedUrl: function () { return pendingUrl; },
    variables: variables,
  };
}

/* ---------------------------------------------------------------- runner */

function runScript(code, contextOptions, randomQueue, frozenMillis) {
  const handle = makePm(contextOptions);
  const FakeDate = function (...args) {
    return args.length ? new Date(...args) : new Date(frozenMillis);
  };
  FakeDate.now = function () { return frozenMillis; };
  FakeDate.parse = Date.parse;
  FakeDate.UTC = Date.UTC;

  const sandbox = {
    pm: handle.pm,
    CryptoJS: makeCryptoJS(randomQueue),
    Date: FakeDate,
    Math: Math,
    JSON: JSON,
    String: String,
    Array: Array,
    Object: Object,
    RegExp: RegExp,
    Error: Error,
    encodeURIComponent: encodeURIComponent,
    decodeURIComponent: decodeURIComponent,
    parseInt: parseInt,
    isNaN: isNaN,
    console: console,
  };
  sandbox.globalThis = sandbox;

  vm.runInNewContext(code, sandbox, { timeout: 5000, filename: 'uvp-signature.js' });
  return handle;
}

function nonceBytes() {
  return Buffer.from('000102030405060708090a0b0c0d0e0f', 'hex');
}

/* ---------------------------------------------------------------- checks */

function extractPostmanScript(collection) {
  const event = (collection.event || []).filter(function (item) {
    return item.listen === 'prerequest';
  })[0];
  if (!event) {
    throw new Error('集合里没有 prerequest 事件');
  }
  return event.script.exec.join('\n');
}

function flatten(items, out) {
  (items || []).forEach(function (item) {
    if (item.item) {
      flatten(item.item, out);
    } else {
      out.push(item);
    }
  });
  return out;
}

function main() {
  if (!fs.existsSync(COLLECTION)) {
    console.error('找不到 ' + COLLECTION + '，先跑 python3 sdk/scripts/gen-http-collection.py');
    process.exit(1);
  }
  if (!fs.existsSync(FIXTURE)) {
    console.error('找不到签名夹具 ' + FIXTURE);
    process.exit(1);
  }

  const collection = JSON.parse(fs.readFileSync(COLLECTION, 'utf8'));
  const fixture = JSON.parse(fs.readFileSync(FIXTURE, 'utf8'));
  const script = extractPostmanScript(collection);

  console.log('集合: ' + path.relative(SDK_ROOT, COLLECTION));
  console.log('脚本: ' + script.split('\n').length + ' 行\n');

  /* 1. 用夹具向量验签：这是"脚本真的算得对"的唯一证据 */
  console.log('夹具向量（脚本原样执行）');
  const baseVars = {
    baseUrl: 'http://127.0.0.1:51002',
    accessKey: fixture.accessKey,
    secretKey: fixture.secretKey,
    audience: fixture.audience,
  };

  fixture.vectors.forEach(function (vector) {
    const pathSegments = vector.path.split('/').filter(Boolean);
    let handle;
    try {
      handle = runScript(
        script,
        {
          method: vector.method,
          path: vector.path,
          pathSegments: pathSegments,
          query: vector.rawQuery,
          rawUrl: 'http://127.0.0.1:51002' + vector.path + (vector.rawQuery ? '?' + vector.rawQuery : ''),
          headers: [],
          body: vector.body === '' ? undefined : vector.body,
          vars: baseVars,
        },
        [nonceBytes()],
        Number(fixture.timestamp) * 1000
      );
    } catch (error) {
      bad('[' + vector.name + '] 脚本执行失败', error.message);
      return;
    }

    console.log('  -- ' + vector.name);
    assertEqual('canonical 与夹具逐字一致', handle.canonical(), vector.canonical);
    assertEqual('签名与夹具一致', handle.header('X-UVP-Signature'), vector.signature);
    assertEqual('X-UVP-Timestamp', handle.header('X-UVP-Timestamp'), fixture.timestamp);
    assertEqual('X-UVP-Nonce', handle.header('X-UVP-Nonce'), fixture.nonce);
    assertEqual('X-UVP-Sign-Version 是 1', handle.header('X-UVP-Sign-Version'), '1');

    if (vector.method === 'GET') {
      assertEqual('GET 不带 Content-Type', handle.header('Content-Type'), null);
    } else {
      assertEqual('Content-Type 恰好 application/json',
        handle.header('Content-Type'), 'application/json');
    }
    if (!vector.rawQuery) {
      assertEqual('无 query 时不改写 URL', handle.updatedUrl(),
        baseVars.baseUrl + vector.path);
    }
  });

  /* 2. 幂等键：占位值要换成新的 32 位 hex，手填的固定值要保留 */
  console.log('\n幂等键');
  const idemPlaceholder = '00000000-0000-4000-8000-000000000001';
  {
    const handle = runScript(script, {
      method: 'POST',
      path: '/openapi/v1/devices/34020000001320000001/ptz/move',
      pathSegments: ['openapi', 'v1', 'devices', '34020000001320000001', 'ptz', 'move'],
      query: '',
      headers: [{ key: 'Idempotency-Key', value: idemPlaceholder }],
      body: '{"action":"stop","speed":0}',
      vars: baseVars,
    }, [nonceBytes(), Buffer.from('aabbccddeeff00112233445566778899', 'hex')], 1790000000000);

    const generated = handle.header('Idempotency-Key');
    assertEqual('占位值被替换成 32 位 hex', /^[0-9a-f]{32}$/.test(generated), true);
    assertEqual('canonical 第 10 行用的是新 key',
      String(handle.canonical()).split('\n')[9], generated);
  }
  {
    const handle = runScript(script, {
      method: 'POST',
      path: '/openapi/v1/devices/34020000001320000001/ptz/move',
      pathSegments: ['openapi', 'v1', 'devices', '34020000001320000001', 'ptz', 'move'],
      query: '',
      headers: [{ key: 'Idempotency-Key', value: 'replay-test-1' }],
      body: '{"action":"stop","speed":0}',
      vars: baseVars,
    }, [nonceBytes()], 1790000000000);

    assertEqual('手填的固定 key 原样保留', handle.header('Idempotency-Key'), 'replay-test-1');
  }
  {
    const handle = runScript(script, {
      method: 'GET',
      path: '/openapi/v1/devices',
      pathSegments: ['openapi', 'v1', 'devices'],
      query: '',
      headers: [],
      vars: baseVars,
    }, [nonceBytes()], 1790000000000);

    assertEqual('无幂等头的接口第 10 行为空',
      String(handle.canonical()).split('\n')[9], '');
  }

  /* 3. 报错路径：宁愿在发出去之前报错，也不要静默 401 */
  console.log('\n参数异常时的行为');
  const common = {
    method: 'GET',
    path: '/openapi/v1/devices',
    pathSegments: ['openapi', 'v1', 'devices'],
    query: '',
    headers: [],
    vars: baseVars,
  };

  assertThrows('缺 audience 直接报错', function () {
    const vars = Object.assign({}, baseVars);
    delete vars.audience;
    runScript(script, Object.assign({}, common, { vars: vars }), [nonceBytes()], 1790000000000);
  }, 'audience');

  assertThrows('SK 不是 32 字节时报错', function () {
    const vars = Object.assign({}, baseVars, { secretKey: 'dG9vLXNob3J0' });
    runScript(script, Object.assign({}, common, { vars: vars }), [nonceBytes()], 1790000000000);
  }, '32 字节');

  assertThrows('query 里的 + 直接报错', function () {
    runScript(script, Object.assign({}, common, { query: 'keyword=a+b' }), [nonceBytes()], 1790000000000);
  }, '+');

  assertThrows('路径变量没替换时报错', function () {
    runScript(script, Object.assign({}, common, { path: '/openapi/v1/devices/{{deviceId}}' }),
      [nonceBytes()], 1790000000000);
  }, '变量');

  assertThrows('GET 带请求体时报错', function () {
    runScript(script, Object.assign({}, common, { body: '{}' }), [nonceBytes()], 1790000000000);
  }, 'GET');

  assertThrows('非 raw 请求体时报错', function () {
    const handle = makePm(Object.assign({}, common, { vars: baseVars }));
    handle.pm.request.body = { mode: 'formdata' };
    const sandbox = {
      pm: handle.pm,
      CryptoJS: makeCryptoJS([nonceBytes()]),
      Date: { now: function () { return 1790000000000; } },
      encodeURIComponent: encodeURIComponent,
      decodeURIComponent: decodeURIComponent,
    };
    sandbox.globalThis = sandbox;
    vm.runInNewContext(script, sandbox, { timeout: 5000 });
  }, 'raw');

  /* 4. 集合本身的自洽性 */
  console.log('\n集合自洽性');
  const requests = flatten(collection.item, []);
  assertEqual('请求数 = 84', requests.length, 84);

  const scriptedHeaders = [];
  requests.forEach(function (item) {
    (item.request.header || []).forEach(function (header) {
      if (SIGNATURE_HEADERS.indexOf(header.key) >= 0) {
        scriptedHeaders.push(item.name + ' -> ' + header.key);
      }
    });
  });
  assertEqual('没有让用户手填的签名头', scriptedHeaders.length, 0);

  let idemCount = 0;
  requests.forEach(function (item) {
    (item.request.header || []).forEach(function (header) {
      if (header.key === 'Idempotency-Key') {
        idemCount += 1;
        if (header.value !== idemPlaceholder) {
          bad('幂等头没有用占位值: ' + item.name);
        }
      }
    });
  });
  assertEqual('幂等头都用了占位值', idemCount > 0, true);

  const missingVars = [];
  requests.forEach(function (item) {
    (item.request.url.path || []).forEach(function (segment) {
      const match = /^\{\{([A-Za-z0-9_]+)\}\}$/.exec(segment);
      if (!match) {
        return;
      }
      const declared = (collection.variable || []).some(function (variable) {
        return variable.key === match[1];
      });
      if (!declared) {
        missingVars.push(item.name + ' -> ' + match[1]);
      }
    });
  });
  assertEqual('路径变量都有集合变量兜底', missingVars.length, 0);

  /* 5. Apifox 文件里的脚本必须和 Postman 那份逐字相同 */
  console.log('\nApifox 一致性');
  if (!fs.existsSync(APIFOX)) {
    bad('找不到 ' + APIFOX);
  } else {
    const apifox = JSON.parse(fs.readFileSync(APIFOX, 'utf8'));
    const folders = apifox.apiCollection || [];
    assertEqual('Apifox 分组数 = ' + folders.length, folders.length > 0, true);

    let mismatched = 0;
    let apiCount = 0;
    folders.forEach(function (folder) {
      const processors = folder.preProcessors || [];
      const custom = processors.filter(function (item) {
        return item.type === 'customScript';
      })[0];
      if (!custom || custom.data !== script) {
        mismatched += 1;
      }
      apiCount += (folder.items || []).length;
    });
    assertEqual('每个分组的签名脚本都与 Postman 逐字相同', mismatched, 0);
    assertEqual('Apifox 接口数 = 84', apiCount, 84);
  }

  console.log('');
  if (failures === 0) {
    console.log('\u2713 全部通过（' + checks + ' 项）');
    return 0;
  }
  console.log('\u2717 ' + failures + ' 项失败 / 共 ' + checks + ' 项');
  return 1;
}

process.exit(main());
