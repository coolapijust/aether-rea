import { connect } from "cloudflare:sockets";

const PROTOCOL_LABEL = "aether-realist-v3";
const RECORD_HEADER_LENGTH = 24;
const TYPE_METADATA = 0x01;
const TYPE_DATA = 0x02;
const TYPE_ERROR = 0x7f;
const DEFAULT_SECRET_PATH = "/aether";
const DEFAULT_LANDING_HTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width,initial-scale=1" />
    <title>Aether Edge Relay</title>
    <style>
      body { font-family: system-ui, sans-serif; margin: 3rem; color: #1f2933; }
      code { background: #f5f7fa; padding: 0.2rem 0.4rem; border-radius: 4px; }
      .card { max-width: 720px; }
    </style>
  </head>
  <body>
    <div class="card">
      <h1>Aether Edge Relay</h1>
      <p>This endpoint hosts a WebTransport-based relay service.</p>
      <p>Public documentation is available upon request.</p>
      <p><strong>Status:</strong> operational</p>
      <p><strong>Contact:</strong> <code>ops@example.com</code></p>
    </div>
  </body>
</html>`;

const ERROR_CODES = {
  badRecord: 0x0001,
  metadataDecrypt: 0x0002,
  unsupported: 0x0003,
  targetConnect: 0x0004,
  streamAbort: 0x0005,
  resourceLimit: 0x0006,
  timeout: 0x0007,
};

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const secretPath = env.SECRET_PATH || DEFAULT_SECRET_PATH;

    if (url.pathname === "/") {
      return new Response(DEFAULT_LANDING_HTML, {
        status: 200,
        headers: { "content-type": "text/html; charset=utf-8" },
      });
    }

    if (url.pathname !== secretPath) {
      return new Response("Not found", { status: 404 });
    }

    if (!request.webTransport) {
      return new Response("WebTransport required", { status: 400 });
    }

    const session = await request.webTransport.accept();
    const streamCounter = createStreamCounter();

    ctx.waitUntil(handleSession(session, env, streamCounter));
    return new Response(null, { status: 200 });
  },
};

async function handleSession(session, env, streamCounter) {
  // 监听 session 关闭
  const sessionClosed = session.closed
    .then(() => ({ closed: true, error: null }))
    .catch((err) => ({ closed: true, error: err }));

  try {
    for await (const stream of session.incomingBidirectionalStreams) {
      const streamId = streamCounter.next().value;
      handleBidirectionalStream(stream, env, streamId).catch((err) => {
        console.error(`Stream ${streamId} error:`, err);
        try {
          stream.readable.cancel();
          stream.writable.abort();
        } catch (e) {
          // Ignore cleanup errors
        }
      });
    }
  } catch (err) {
    console.error("Session incoming streams error:", err);
  }

  // 等待 session 完全关闭
  await sessionClosed;
  console.log("Session closed");
}

function createStreamCounter() {
  let current = 0;
  return {
    next() {
      current += 1;
      return { value: current, done: false };
    },
  };
}

async function handleBidirectionalStream(stream, env, streamId) {
  const reader = createBufferedReader(stream.readable);
  const writer = stream.writable.getWriter();

  let targetInfo;
  try {
    const record = await readRecord(reader);
    if (!record || record.type !== TYPE_METADATA) {
      await writeError(writer, ERROR_CODES.badRecord, "metadata required");
      return;
    }

    targetInfo = await decryptMetadata(record, env.PSK, streamId);
  } catch (error) {
    await writeError(writer, ERROR_CODES.metadataDecrypt, "metadata decrypt failed");
    return;
  }

  let socket;
  try {
    socket = connect({ hostname: targetInfo.host, port: targetInfo.port });
  } catch (error) {
    await writeError(writer, ERROR_CODES.targetConnect, "connect failed");
    return;
  }

  const outbound = pumpClientToSocket(reader, socket.writable);
  const inbound = pumpSocketToClient(socket.readable, writer, targetInfo.options);

  await Promise.race([outbound, inbound]).catch(async () => {
    await writeError(writer, ERROR_CODES.streamAbort, "stream aborted");
  });

  await closeResources(reader, writer, socket);
}

async function pumpClientToSocket(reader, socketWritable) {
  const socketWriter = socketWritable.getWriter();
  try {
    while (true) {
      const record = await readRecord(reader);
      if (!record) {
        break;
      }
      if (record.type !== TYPE_DATA) {
        continue;
      }
      await socketWriter.ready;
      await socketWriter.write(record.payload);
    }
  } finally {
    socketWriter.releaseLock();
  }
}

async function pumpSocketToClient(socketReadable, clientWriter, options) {
  const reader = socketReadable.getReader();
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      const record = buildDataRecord(value, options);
      await clientWriter.ready;
      await clientWriter.write(record);
    }
  } finally {
    reader.releaseLock();
  }
}

async function readRecord(reader) {
  const lengthBytes = await reader.readExact(4);
  if (!lengthBytes) {
    return null;
  }
  const totalLength = readUint32(lengthBytes, 0);
  if (totalLength < RECORD_HEADER_LENGTH) {
    return null;
  }
  const recordBytes = await reader.readExact(totalLength);
  if (!recordBytes) {
    return null;
  }

  const type = recordBytes[0];
  const payloadLength = readUint32(recordBytes, 4);
  const paddingLength = readUint32(recordBytes, 8);
  const iv = recordBytes.slice(12, 24);
  const payloadStart = RECORD_HEADER_LENGTH;
  const payloadEnd = payloadStart + payloadLength;

  if (payloadEnd + paddingLength !== recordBytes.length) {
    return null;
  }

  return {
    type,
    payload: recordBytes.slice(payloadStart, payloadEnd),
    iv,
    header: recordBytes.slice(0, RECORD_HEADER_LENGTH),
  };
}

function buildDataRecord(payload, options = {}) {
  const paddingLength = selectPadding(options, payload.byteLength);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const totalLength = RECORD_HEADER_LENGTH + payload.byteLength + paddingLength;
  const record = new Uint8Array(4 + totalLength);

  writeUint32(record, 0, totalLength);
  record[4] = TYPE_DATA;
  record[5] = 0x00;
  record[6] = 0x00;
  record[7] = 0x00;
  writeUint32(record, 8, payload.byteLength);
  writeUint32(record, 12, paddingLength);
  record.set(iv, 16);
  record.set(payload, 4 + RECORD_HEADER_LENGTH);

  if (paddingLength > 0) {
    const padding = crypto.getRandomValues(new Uint8Array(paddingLength));
    record.set(padding, 4 + RECORD_HEADER_LENGTH + payload.byteLength);
  }

  return record;
}

function selectPadding(options, payloadLength) {
  const maxPadding = Number.isFinite(options.maxPadding)
    ? options.maxPadding
    : 0;
  const minPadMax = Math.min(32, maxPadding > 0 ? maxPadding : 32);
  const minPadding = 1 + Math.floor(Math.random() * minPadMax);
  if (maxPadding <= 0 || maxPadding <= minPadding) {
    return minPadding;
  }
  const limit = Math.min(maxPadding - minPadding, 255 + payloadLength);
  const extra = limit > 0 ? Math.floor(Math.random() * limit) : 0;
  return minPadding + extra;
}

async function decryptMetadata(record, psk, streamId) {
  if (!psk) {
    throw new Error("missing psk");
  }
  const key = await deriveKey(psk, streamId);
  const plaintext = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: record.iv, additionalData: record.header },
    key,
    record.payload
  );
  return parseMetadata(new Uint8Array(plaintext));
}

async function deriveKey(psk, streamId) {
  const rawKey = typeof psk === "string" ? new TextEncoder().encode(psk) : psk;
  const baseKey = await crypto.subtle.importKey(
    "raw",
    rawKey,
    "HKDF",
    false,
    ["deriveKey"]
  );
  const info = new TextEncoder().encode(String(streamId));
  return crypto.subtle.deriveKey(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: new TextEncoder().encode(PROTOCOL_LABEL),
      info,
    },
    baseKey,
    { name: "AES-GCM", length: 128 },
    false,
    ["decrypt"]
  );
}

function parseMetadata(buffer) {
  const addressType = buffer[0];
  const port = readUint16(buffer, 1);
  let offset = 3;
  let host;

  if (addressType === 0x01) {
    host = Array.from(buffer.slice(offset, offset + 4)).join(".");
    offset += 4;
  } else if (addressType === 0x02) {
    const segments = [];
    for (let i = 0; i < 16; i += 2) {
      segments.push(bufferToHex(buffer.slice(offset + i, offset + i + 2)));
    }
    host = segments.join(":");
    offset += 16;
  } else if (addressType === 0x03) {
    const length = buffer[offset];
    offset += 1;
    host = new TextDecoder().decode(buffer.slice(offset, offset + length));
    offset += length;
  } else {
    throw new Error("unsupported address type");
  }

  const optionsLength = readUint16(buffer, offset);
  offset += 2;
  const optionsPayload = buffer.slice(offset, offset + optionsLength);
  return {
    host,
    port,
    options: parseOptions(optionsPayload),
  };
}

function parseOptions(buffer) {
  const options = {};
  let offset = 0;
  while (offset + 2 <= buffer.length) {
    const type = buffer[offset];
    const length = buffer[offset + 1];
    offset += 2;
    if (offset + length > buffer.length) {
      break;
    }
    const value = buffer.slice(offset, offset + length);
    offset += length;
    if (type === 0x01) {
      options.maxPadding = readUint16(value, 0);
    }
  }
  return options;
}

async function writeError(writer, code, message) {
  const messageBytes = new TextEncoder().encode(message);
  const payload = new Uint8Array(4 + messageBytes.byteLength);
  writeUint16(payload, 0, code);
  payload.set(messageBytes, 4);

  const iv = new Uint8Array(12);
  const totalLength = RECORD_HEADER_LENGTH + payload.byteLength;
  const record = new Uint8Array(4 + totalLength);
  writeUint32(record, 0, totalLength);
  record[4] = TYPE_ERROR;
  record[5] = 0x00;
  record[6] = 0x00;
  record[7] = 0x00;
  writeUint32(record, 8, payload.byteLength);
  writeUint32(record, 12, 0);
  record.set(iv, 16);
  record.set(payload, 4 + RECORD_HEADER_LENGTH);

  await writer.ready;
  await writer.write(record);
}

function createBufferedReader(readable) {
  const reader = readable.getReader();
  let stash = new Uint8Array(0);

  return {
    async readExact(length) {
      while (stash.byteLength < length) {
        const { done, value } = await reader.read();
        if (done) {
          return null;
        }
        stash = concatUint8Arrays(stash, value);
      }
      const chunk = stash.slice(0, length);
      stash = stash.slice(length);
      return chunk;
    },
    release() {
      reader.releaseLock();
    },
    cancel() {
      return reader.cancel();
    },
  };
}

function concatUint8Arrays(a, b) {
  const buffer = new Uint8Array(a.byteLength + b.byteLength);
  buffer.set(a, 0);
  buffer.set(b, a.byteLength);
  return buffer;
}

async function closeResources(reader, writer, socket) {
  try {
    reader.release();
  } catch (error) {
    // ignored
  }
  try {
    await writer.close();
  } catch (error) {
    // ignored
  }
  try {
    socket.close();
  } catch (error) {
    // ignored
  }
}

function readUint32(buffer, offset) {
  return (
    (buffer[offset] << 24) |
    (buffer[offset + 1] << 16) |
    (buffer[offset + 2] << 8) |
    buffer[offset + 3]
  ) >>> 0;
}

function readUint16(buffer, offset) {
  return (buffer[offset] << 8) | buffer[offset + 1];
}

function writeUint32(buffer, offset, value) {
  buffer[offset] = (value >>> 24) & 0xff;
  buffer[offset + 1] = (value >>> 16) & 0xff;
  buffer[offset + 2] = (value >>> 8) & 0xff;
  buffer[offset + 3] = value & 0xff;
}

function writeUint16(buffer, offset, value) {
  buffer[offset] = (value >>> 8) & 0xff;
  buffer[offset + 1] = value & 0xff;
}

function bufferToHex(buffer) {
  return Array.from(buffer)
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("");
}
