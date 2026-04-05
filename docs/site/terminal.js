(function () {
  'use strict';

  var commands = [];
  var terminalOutput = null;
  var terminalEl = null;
  var paused = false;
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

  function schedule(fn, delay) {
    pendingTimer = setTimeout(function () {
      pendingTimer = null;
      if (!paused) {
        fn();
      } else {
        var waitForUnpause = function () {
          pendingTimer = setTimeout(function () {
            pendingTimer = null;
            if (!paused) {
              fn();
            } else {
              waitForUnpause();
            }
          }, 100);
        };
        waitForUnpause();
      }
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
    commandSpan.textContent = item.command;
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

    typeChars(item.command, commandSpan, 0);
  }

  function typeChars(text, commandSpan, charIndex) {
    if (charIndex < text.length) {
      commandSpan.textContent += text[charIndex];
      schedule(function () {
        typeChars(text, commandSpan, charIndex + 1);
      }, 40);
    } else {
      terminalEl.classList.remove('typing');
      schedule(startCommand, 2000);
    }
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

    terminalEl.addEventListener('mouseenter', function () { paused = true; });
    terminalEl.addEventListener('mouseleave', function () { paused = false; });

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
