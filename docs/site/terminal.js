(function () {
  'use strict';

  var commands = [];
  var terminalOutput = null;
  var terminalEl = null;
  var pendingTimer = null;

  // Commands are grouped by product and picked in round-robin order.
  // Each product cycles through its commands sequentially, shuffled on first load.
  var productQueues = {};  // { pro: [...], protect: [...] }
  var productOrder = [];   // ['pro', 'protect', ...] — discovered from data
  var productIndex = 0;    // which product to pick from next
  var queuePositions = {}; // { pro: 0, protect: 0 }

  function clearOutput() {
    terminalOutput.textContent = '';
  }

  // Syntax highlighting: the product namespace token wears its product hue
  // (same wayfinding system as the catalog), flags dim. Everything else
  // stays default. Matches the .tok-* classes in style.css.
  var NAMESPACE_CLASSES = {
    pro: 'tok-product-pro',
    protect: 'tok-product-protect',
    school: 'tok-product-school',
    security: 'tok-product-security',
    platform: 'tok-product-platform'
  };

  // Split "jamf-cli pro comp list -o table" into [{text, cls}] segments.
  // Word 0 is the binary, word 1 the product namespace, "-"-prefixed words
  // are flags; each segment keeps its leading space so typing stays 1:1.
  function segmentCommand(text) {
    var words = text.split(' ');
    var segs = [];
    for (var i = 0; i < words.length; i++) {
      var cls = null;
      if (i === 1 && NAMESPACE_CLASSES[words[i]]) {
        cls = NAMESPACE_CLASSES[words[i]];
      } else if (words[i].charAt(0) === '-') {
        cls = 'tok-flag';
      }
      segs.push({ text: (i > 0 ? ' ' : '') + words[i], cls: cls });
    }
    return segs;
  }

  // Render a full command into container as highlighted spans (static path).
  function renderCommand(container, text) {
    var segs = segmentCommand(text);
    for (var i = 0; i < segs.length; i++) {
      var span = document.createElement('span');
      if (segs[i].cls) span.className = segs[i].cls;
      span.textContent = segs[i].text;
      container.appendChild(span);
    }
  }

  function schedule(fn, delay) {
    pendingTimer = setTimeout(function () {
      pendingTimer = null;
      fn();
    }, delay);
  }

  function shuffle(arr) {
    for (var i = arr.length - 1; i > 0; i--) {
      var j = Math.floor(Math.random() * (i + 1));
      var tmp = arr[i];
      arr[i] = arr[j];
      arr[j] = tmp;
    }
    return arr;
  }

  function nextCommand() {
    if (productOrder.length === 0) return commands[0];

    var product = productOrder[productIndex % productOrder.length];
    productIndex++;

    var queue = productQueues[product];
    var pos = queuePositions[product] || 0;
    var cmd = queue[pos % queue.length];
    queuePositions[product] = pos + 1;

    return { command: 'jamf-cli ' + cmd.command };
  }

  // ===== Reduced-motion path =====

  function showStatic() {
    var item = nextCommand();
    clearOutput();

    var cmdLine = document.createElement('div');
    var promptSpan = document.createElement('span');
    promptSpan.className = 'prompt';
    promptSpan.textContent = '$ ';
    var commandSpan = document.createElement('span');
    commandSpan.className = 'command';
    renderCommand(commandSpan, item.command);
    cmdLine.appendChild(promptSpan);
    cmdLine.appendChild(commandSpan);
    terminalOutput.appendChild(cmdLine);

    schedule(showStatic, 5000);
  }

  // ===== Normal (animated) path =====

  function startCommand() {
    var item = nextCommand();
    clearOutput();
    terminalEl.classList.add('typing');

    var cmdLine = document.createElement('div');
    var promptSpan = document.createElement('span');
    promptSpan.className = 'prompt';
    promptSpan.textContent = '$ ';
    var commandSpan = document.createElement('span');
    commandSpan.className = 'command';
    cmdLine.appendChild(promptSpan);
    cmdLine.appendChild(commandSpan);
    terminalOutput.appendChild(cmdLine);

    typeChars(segmentCommand(item.command), commandSpan, 0, 0, null);
  }

  // Types char-by-char across highlighted segments: a new span is created
  // when typing enters a segment, then filled one character at a time —
  // the color arrives with the token, just like a live shell.
  function typeChars(segs, commandSpan, segIndex, charIndex, currentSpan) {
    if (segIndex >= segs.length) {
      terminalEl.classList.remove('typing');
      schedule(startCommand, 3500);
      return;
    }
    var seg = segs[segIndex];
    if (charIndex === 0) {
      currentSpan = document.createElement('span');
      if (seg.cls) currentSpan.className = seg.cls;
      commandSpan.appendChild(currentSpan);
    }
    currentSpan.textContent += seg.text[charIndex];
    var nextSeg = segIndex;
    var nextChar = charIndex + 1;
    if (nextChar >= seg.text.length) {
      nextSeg = segIndex + 1;
      nextChar = 0;
      currentSpan = null;
    }
    schedule(function () {
      typeChars(segs, commandSpan, nextSeg, nextChar, currentSpan);
    }, 55);
  }

  // ===== Build command queues from commands.json =====

  function buildQueues(cmds) {
    var byProduct = {};

    for (var i = 0; i < cmds.length; i++) {
      var p = cmds[i].product;
      if (!p) continue;
      if (!byProduct[p]) byProduct[p] = [];
      // Only include leaf commands that have interesting names (skip generic CRUD)
      byProduct[p].push(cmds[i]);
    }

    // For each product, pick a diverse set: prefer commands with flags or short paths
    var order = Object.keys(byProduct).sort();
    for (var k = 0; k < order.length; k++) {
      var product = order[k];
      var all = byProduct[product];

      // Prioritize: commands with 2-3 path segments (not deeply nested CRUD)
      var interesting = all.filter(function (c) {
        var depth = c.command.split(' ').length;
        return depth <= 2;
      });

      // Fall back to all commands if not enough interesting ones
      if (interesting.length < 5) interesting = all;

      productQueues[product] = shuffle(interesting.slice());
      queuePositions[product] = 0;
    }

    productOrder = order;
    productIndex = 0;
  }

  // ===== Initialization =====

  function init() {
    terminalOutput = document.getElementById('terminal-output');
    terminalEl = document.getElementById('terminal');
    if (!terminalOutput || !terminalEl) return;


    // Try to load from commands.json (same file the catalog uses)
    fetch('commands.json')
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.json();
      })
      .then(function (data) {
        if (data.commands && data.commands.length > 0) {
          buildQueues(data.commands);
        }
      })
      .catch(function () {
        // Fallback: use the inline terminal-data if commands.json fails
        var dataEl = document.getElementById('terminal-data');
        if (dataEl) {
          try {
            commands = JSON.parse(dataEl.textContent);
          } catch (e) { /* ignore */ }
        }
      })
      .finally(function () {
        // If no queues were built and no fallback commands, bail
        if (productOrder.length === 0 && commands.length === 0) return;

        var reducedMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        if (reducedMotion) {
          showStatic();
        } else {
          startCommand();
        }
      });
  }

  document.addEventListener('DOMContentLoaded', init);
})();
