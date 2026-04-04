(function () {
  'use strict';

  var commands = [];
  var terminalOutput = null;
  var terminalEl = null;
  var currentIndex = 0;
  var paused = false;
  var pendingTimer = null;

  // Parse ANSI bold sequences into DOM nodes.
  // Handles ESC[1m...ESC[0m as <strong>, strips other ANSI escape codes.
  // Uses indexOf/search to walk the string without calling regex .exec().
  function renderAnsiToDom(container, text) {
    var pos = 0;

    while (pos < text.length) {
      var remaining = text.slice(pos);
      var nextAnsi = remaining.search(/\u001b\[\d+m/);

      if (nextAnsi === -1) {
        // No more escape codes — emit the rest as plain text
        container.appendChild(document.createTextNode(remaining));
        break;
      }

      // Emit text before the escape sequence
      if (nextAnsi > 0) {
        container.appendChild(document.createTextNode(remaining.slice(0, nextAnsi)));
      }

      var codeStart = pos + nextAnsi;

      if (text.indexOf('\u001b[1m', codeStart) === codeStart) {
        // Bold sequence: ESC[1m...ESC[0m
        var boldStart = codeStart + 4; // length of ESC[1m
        var closeIdx = text.indexOf('\u001b[0m', boldStart);
        if (closeIdx !== -1) {
          var strong = document.createElement('strong');
          strong.textContent = text.slice(boldStart, closeIdx);
          container.appendChild(strong);
          pos = closeIdx + 4; // length of ESC[0m
        } else {
          // No closing code — emit the rest literally
          container.appendChild(document.createTextNode(text.slice(codeStart)));
          break;
        }
      } else {
        // Other ANSI code — strip it by advancing past ESC[Xm
        var codeMatch = text.slice(codeStart).match(/^\u001b\[\d+m/);
        pos = codeStart + (codeMatch ? codeMatch[0].length : 3);
      }
    }
  }

  function clearOutput() {
    while (terminalOutput.firstChild) {
      terminalOutput.removeChild(terminalOutput.firstChild);
    }
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

  // ===== Reduced-motion path =====

  function showStatic(index) {
    var item = commands[index];
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

    item.output.forEach(function (line) {
      var div = document.createElement('div');
      renderAnsiToDom(div, line);
      terminalOutput.appendChild(div);
    });

    schedule(function () {
      showStatic((index + 1) % commands.length);
    }, 5000);
  }

  // ===== Normal (animated) path =====

  function startCommand(index) {
    var item = commands[index];
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

    typeCommand(item, commandSpan, 0);
  }

  function typeCommand(item, commandSpan, charIndex) {
    if (charIndex < item.command.length) {
      commandSpan.textContent += item.command[charIndex];
      schedule(function () {
        typeCommand(item, commandSpan, charIndex + 1);
      }, 40);
    } else {
      terminalEl.classList.remove('typing');
      schedule(function () {
        revealOutput(item, 0);
      }, 300);
    }
  }

  function revealOutput(item, lineIndex) {
    if (lineIndex < item.output.length) {
      var div = document.createElement('div');
      renderAnsiToDom(div, item.output[lineIndex]);
      terminalOutput.appendChild(div);
      schedule(function () {
        revealOutput(item, lineIndex + 1);
      }, 200);
    } else {
      schedule(function () {
        currentIndex = (currentIndex + 1) % commands.length;
        startCommand(currentIndex);
      }, 3000);
    }
  }

  // ===== Initialization =====

  function init() {
    var dataEl = document.getElementById('terminal-data');
    if (!dataEl) return;

    try {
      commands = JSON.parse(dataEl.textContent);
    } catch (e) {
      return;
    }

    if (!commands || commands.length === 0) return;

    terminalOutput = document.getElementById('terminal-output');
    terminalEl = document.getElementById('terminal');
    if (!terminalOutput || !terminalEl) return;

    terminalEl.addEventListener('mouseenter', function () {
      paused = true;
    });
    terminalEl.addEventListener('mouseleave', function () {
      paused = false;
    });

    var reducedMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reducedMotion) {
      showStatic(0);
      return;
    }

    startCommand(0);
  }

  document.addEventListener('DOMContentLoaded', init);
})();
