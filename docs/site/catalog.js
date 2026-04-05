(function () {
  'use strict';

  var GROUP_ORDER = [
    'Getting Started',
    'Configuration',
    'Shell Completion',
    'Utilities',
    'Power Commands',
    'Computer Management',
    'Mobile Device Management',
    'Enrollment',
    'Inventory & Search',
    'Organization',
    'Users & Security',
    'Content & Configuration',
    'MDM & Certificates',
    'Server Administration',
    'Classic - Computers',
    'Classic - Mobile Devices',
    'Classic - Configuration',
    'Classic - Administration',
    'Classic - Patch Management',
    'Security Configuration',
    'Endpoints',
    'Access & Identity'
  ];

  var JAMF_ICON_SVG = '<svg class="product-icon" viewBox="0 0 43 43" fill="none" xmlns="http://www.w3.org/2000/svg"><path fill-rule="evenodd" clip-rule="evenodd" d="M33.63 6.96c-5.52 0-8.78 2.33-10.27 7.31l-3.84 11.88c-1.41 3.91-3.68 5.52-7.82 5.52H2.19A2.19 2.19 0 000 33.87v6.06c0 1.16.94 2.1 2.1 2.1h38.57a2.19 2.19 0 002.19-2.19V9.08c0-1.17-.95-2.12-2.11-2.12h-7.12z" fill="currentColor"/><path fill-rule="evenodd" clip-rule="evenodd" d="M2.35 0A2.35 2.35 0 000 2.35v16.22a2.29 2.29 0 002.29 2.29h8.17c3.74 0 8.88-.77 10.29-7.41l2.23-10.6A2.35 2.35 0 0020.68 0H2.35z" fill="currentColor"/></svg>';

  var allCommands = [];
  var newCommandSet = {};
  var heroExamples = {};
  var commandExamples = {};
  var activeProduct = 'all';
  var expandedCommand = null;
  var expandHintShown = false;
  var currentSearchQuery = '';

  // ===== Initialization =====

  function init() {
    loadHeroExamples();
    loadExamples();
    setupSearch();
    setupTabs();
    setupStatCards();
    setupToggleAll();
    setupKeyboardNav();
    setupNavScroll();
    setupCopyButtons();
    setupDeepLinking();
    fetchCommands();
  }

  function loadHeroExamples() {
    var el = document.getElementById('hero-examples');
    if (el) {
      try {
        heroExamples = JSON.parse(el.textContent);
      } catch (e) {
        heroExamples = {};
      }
    }
  }

  function loadExamples() {
    fetch('examples.json')
      .then(function (res) { return res.ok ? res.json() : {}; })
      .then(function (data) { commandExamples = data || {}; })
      .catch(function () { commandExamples = {}; });
  }

  function fetchCommands() {
    fetch('commands.json')
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.json();
      })
      .then(function (data) {
        allCommands = reclassifyCoreCommands(data.commands || []);
        if (data.newCommands) {
          for (var n = 0; n < data.newCommands.length; n++) {
            newCommandSet[data.newCommands[n]] = true;
          }
        }
        populateStats(data);
        renderCatalog(allCommands, '', activeProduct);
        hideCatalogLoading();
        handleDeepLink();
      })
      .catch(function (err) {
        var catalog = document.getElementById('catalog');
        if (catalog) {
          catalog.innerHTML = '<p style="text-align:center;color:var(--text-secondary);padding:2rem 0;">Failed to load commands. ' +
            '<a href="https://github.com/Jamf-Concepts/jamf-cli">View on GitHub</a> instead.</p>';
        }
        hideCatalogLoading();
      });
  }

  function hideCatalogLoading() {
    var loading = document.getElementById('catalog-loading');
    if (loading) loading.style.display = 'none';
  }

  function populateStats(data) {
    var count = data.commandCount || data.commands.length;
    var version = data.version || '';
    var generatedAt = data.generatedAt || '';

    setText('command-count', count.toLocaleString());
    setText('stat-commands', count.toLocaleString());

    var search = document.getElementById('search');
    if (search) search.placeholder = 'Search ' + count.toLocaleString() + ' commands... (press / to focus)';

    var proCount = 0;
    var protectCount = 0;
    for (var i = 0; i < data.commands.length; i++) {
      if (data.commands[i].product === 'pro') proCount++;
      else if (data.commands[i].product === 'protect') protectCount++;
    }
    var coreCount = count - proCount - protectCount;
    setText('stat-pro', proCount.toLocaleString());
    setText('stat-protect', protectCount.toLocaleString());
    setText('stat-core', coreCount.toLocaleString());

    // Count unique top-level resources (second path segment, e.g. "pro computers list" → "computers")
    var resources = {};
    for (var r = 0; r < data.commands.length; r++) {
      var parts = data.commands[r].command.split(' ');
      if (parts.length >= 2) {
        var key = parts.slice(0, 2).join(' ');
        resources[key] = true;
      }
    }
    setText('stat-resources', Object.keys(resources).length.toLocaleString());

    var versionBadge = document.getElementById('version-badge');
    if (versionBadge && version) {
      versionBadge.textContent = 'v' + version;
    }

    var lastUpdated = document.getElementById('last-updated');
    if (lastUpdated && generatedAt) {
      lastUpdated.textContent = 'Updated ' + formatDate(generatedAt);
    }

    var footerVersion = document.getElementById('footer-version');
    if (footerVersion && version) {
      footerVersion.textContent = ' · v' + version;
    }
  }

  function setText(id, text) {
    var el = document.getElementById(id);
    if (el) {
      el.textContent = text;
      el.classList.add('loaded');
    }
  }

  function formatDate(iso) {
    try {
      var d = new Date(iso);
      return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
    } catch (e) {
      return iso;
    }
  }

  function highlightText(text, parent) {
    if (!currentSearchQuery || !text) {
      parent.textContent = text;
      return;
    }
    var lower = text.toLowerCase();
    var idx = lower.indexOf(currentSearchQuery);
    if (idx === -1) {
      parent.textContent = text;
      return;
    }
    parent.textContent = '';
    if (idx > 0) parent.appendChild(document.createTextNode(text.slice(0, idx)));
    var mark = document.createElement('mark');
    mark.className = 'search-highlight';
    mark.textContent = text.slice(idx, idx + currentSearchQuery.length);
    parent.appendChild(mark);
    if (idx + currentSearchQuery.length < text.length) {
      parent.appendChild(document.createTextNode(text.slice(idx + currentSearchQuery.length)));
    }
  }

  // ===== Rendering =====

  function renderCatalog(commands, searchQuery, productFilter) {
    var catalog = document.getElementById('catalog');
    if (!catalog) return;

    currentSearchQuery = (searchQuery || '').trim().toLowerCase();
    var filtered = filterCommands(commands, searchQuery, productFilter);
    var groups = groupCommands(filtered);
    var sorted = sortGroups(groups);

    catalog.innerHTML = '';

    if (sorted.length === 0) {
      catalog.innerHTML = '<p style="text-align:center;color:var(--text-secondary);padding:2rem 0;">No commands match your search.</p>';
      return;
    }

    var hasSearch = !!(searchQuery && searchQuery.trim());

    // Split groups into shared (no product) and per-product
    // Multi-product groups get split: commands go under each product separately
    var sharedGroups = [];
    var productGroups = {};
    for (var i = 0; i < sorted.length; i++) {
      var byProduct = {};
      var hasNoProduct = false;
      for (var j = 0; j < sorted[i].commands.length; j++) {
        var p = sorted[i].commands[j].product;
        if (p) {
          if (!byProduct[p]) byProduct[p] = [];
          byProduct[p].push(sorted[i].commands[j]);
        } else {
          hasNoProduct = true;
        }
      }
      var prodKeys = Object.keys(byProduct);

      if (prodKeys.length === 0) {
        // All commands have no product — truly shared
        sharedGroups.push(sorted[i]);
      } else if (prodKeys.length === 1 && !hasNoProduct) {
        // All commands are one product
        var prod = prodKeys[0];
        if (!productGroups[prod]) productGroups[prod] = [];
        productGroups[prod].push(sorted[i]);
      } else {
        // Mixed products or mix of product + no-product — split by product
        if (hasNoProduct) {
          var noProdCmds = sorted[i].commands.filter(function (c) { return !c.product; });
          sharedGroups.push({ name: sorted[i].name, commands: noProdCmds });
        }
        for (var pk = 0; pk < prodKeys.length; pk++) {
          var prd = prodKeys[pk];
          if (!productGroups[prd]) productGroups[prd] = [];
          productGroups[prd].push({ name: sorted[i].name, commands: byProduct[prd] });
        }
      }
    }

    // Render shared groups first (no divider)
    for (var s = 0; s < sharedGroups.length; s++) {
      catalog.appendChild(renderGroup(sharedGroups[s], hasSearch));
    }

    // Render each product's groups under a divider
    var productOrder = Object.keys(PRODUCT_LABELS);
    for (var pi = 0; pi < productOrder.length; pi++) {
      var prod = productOrder[pi];
      if (!productGroups[prod] || productGroups[prod].length === 0) continue;
      catalog.appendChild(renderProductDivider(prod));
      for (var g = 0; g < productGroups[prod].length; g++) {
        var expandGroup = hasSearch || productGroups[prod][g].name === 'Getting Started';
        catalog.appendChild(renderGroup(productGroups[prod][g], expandGroup));
      }
    }

    // Any remaining products not in PRODUCT_LABELS
    var allProds = Object.keys(productGroups);
    for (var r = 0; r < allProds.length; r++) {
      if (PRODUCT_LABELS[allProds[r]]) continue;
      catalog.appendChild(renderProductDivider(allProds[r]));
      for (var rg = 0; rg < productGroups[allProds[r]].length; rg++) {
        catalog.appendChild(renderGroup(productGroups[allProds[r]][rg], hasSearch));
      }
    }
  }

  var PRODUCT_LABELS = {
    pro: 'Jamf Pro',
    protect: 'Jamf Protect'
    // school: 'Jamf School',
    // connect: 'Jamf Connect'
  };

  function renderProductDivider(product) {
    var divider = document.createElement('div');
    divider.className = 'product-divider';
    divider.setAttribute('data-product', product);

    var label = document.createElement('span');
    label.className = 'product-divider-label';
    label.innerHTML = JAMF_ICON_SVG + ' ' + (PRODUCT_LABELS[product] || product);
    divider.appendChild(label);

    return divider;
  }

  // Detect when a command references another product (e.g., "pro jamf-protect-plans" → protect)
  // Uses word-boundary matching to avoid false positives like "protect" matching "pro"
  function detectRelatedProduct(cmd) {
    if (!cmd.product || !cmd.command) return null;
    var productNames = Object.keys(PRODUCT_LABELS);
    // Strip the product prefix to avoid self-matching (e.g., "protect ..." shouldn't match "pro")
    var parts = cmd.command.split(' ');
    var remainder = parts.slice(1).join(' ').toLowerCase();
    for (var i = 0; i < productNames.length; i++) {
      var other = productNames[i];
      if (other === cmd.product) continue;
      // Match as a whole word or hyphenated segment (e.g., "jamf-protect" contains "protect")
      var re = new RegExp('(^|[^a-z])' + other + '([^a-z]|$)');
      if (re.test(remainder)) return other;
    }
    return null;
  }

  // Reclassify "Core Commands" into more meaningful subgroups
  var GETTING_STARTED_ORDER = ['setup', 'overview', 'device', 'auth'];

  var CORE_SUBGROUPS = {
    'completion': 'Shell Completion',
    'config': 'Configuration'
  };

  function reclassifyCoreCommands(commands) {
    return commands.map(function (cmd) {
      if (cmd.group !== 'Core Commands') return cmd;

      // Product-specific core → "Getting Started" under that product
      if (cmd.product) {
        var parts = cmd.command.split(' ');
        var action = parts[parts.length - 1];
        var order = GETTING_STARTED_ORDER.indexOf(action);
        if (order === -1) order = 99;
        return Object.assign({}, cmd, { group: 'Getting Started', _gsOrder: order });
      }

      // Non-product core → split by first command segment
      var firstWord = cmd.command.split(' ')[0];
      var subgroup = CORE_SUBGROUPS[firstWord];
      if (subgroup) {
        var shellOrder = cmd.command === 'completion install' ? 0 : 1;
        return Object.assign({}, cmd, { group: subgroup, _shellOrder: shellOrder });
      }

      // Everything else → Utilities
      return Object.assign({}, cmd, { group: 'Utilities' });
    }).sort(function (a, b) {
      if (a.group === 'Getting Started' && b.group === 'Getting Started') {
        return (a._gsOrder || 99) - (b._gsOrder || 99);
      }
      if (a.group === 'Shell Completion' && b.group === 'Shell Completion') {
        var diff = (a._shellOrder === undefined ? 1 : a._shellOrder) - (b._shellOrder === undefined ? 1 : b._shellOrder);
        if (diff !== 0) return diff;
        return a.command.localeCompare(b.command);
      }
      return 0;
    });
  }

  function filterCommands(commands, query, product) {
    var results = commands;

    if (product && product !== 'all') {
      results = results.filter(function (cmd) {
        return cmd.product === product;
      });
    }

    if (query) {
      var q = query.toLowerCase();
      results = results.filter(function (cmd) {
        if (cmd.command.toLowerCase().indexOf(q) !== -1) return true;
        if (cmd.description && cmd.description.toLowerCase().indexOf(q) !== -1) return true;
        if (cmd.aliases) {
          for (var a = 0; a < cmd.aliases.length; a++) {
            if (cmd.aliases[a].toLowerCase().indexOf(q) !== -1) return true;
          }
        }
        if (cmd.flags) {
          for (var f = 0; f < cmd.flags.length; f++) {
            if (cmd.flags[f].toLowerCase().indexOf(q) !== -1) return true;
          }
        }
        return false;
      });
    }

    return results;
  }

  function groupCommands(commands) {
    var map = {};
    for (var i = 0; i < commands.length; i++) {
      var group = commands[i].group || 'Other';
      if (!map[group]) map[group] = [];
      map[group].push(commands[i]);
    }
    return map;
  }

  function sortGroups(groupMap) {
    var result = [];
    for (var i = 0; i < GROUP_ORDER.length; i++) {
      var name = GROUP_ORDER[i];
      if (groupMap[name]) {
        result.push({ name: name, commands: groupMap[name] });
        delete groupMap[name];
      }
    }
    // Append any groups not in the fixed order
    var remaining = Object.keys(groupMap).sort();
    for (var j = 0; j < remaining.length; j++) {
      result.push({ name: remaining[j], commands: groupMap[remaining[j]] });
    }
    return result;
  }

  function renderGroup(group, startExpanded) {
    var container = document.createElement('div');
    container.className = 'catalog-group';
    if (group.name === 'Getting Started') container.classList.add('getting-started');

    // Determine which products are in this group
    var products = {};
    for (var p = 0; p < group.commands.length; p++) {
      var prod = group.commands[p].product;
      if (prod) products[prod] = true;
    }
    var productKeys = Object.keys(products);
    if (productKeys.length === 1) {
      container.setAttribute('data-product', productKeys[0]);
    }

    var header = document.createElement('div');
    header.className = 'catalog-group-header';
    header.setAttribute('role', 'button');
    var expanded = !!startExpanded;
    header.setAttribute('aria-expanded', String(expanded));
    header.setAttribute('tabindex', '0');

    var chevron = document.createElement('span');
    chevron.className = 'group-chevron';
    chevron.textContent = '\u25B6';
    header.appendChild(chevron);

    var headerText = document.createTextNode(' ' + group.name + ' ');
    header.appendChild(headerText);

    var countBadge = document.createElement('span');
    countBadge.className = 'group-count-badge';
    countBadge.textContent = group.commands.length;
    header.appendChild(countBadge);

    // Show product badges if group has commands from multiple products, or for Core/shared groups
    if (productKeys.length > 1 || (productKeys.length === 1 && group.name === 'Core Commands')) {
      var badgeWrap = document.createElement('span');
      badgeWrap.className = 'header-badges';
      for (var b = 0; b < productKeys.length; b++) {
        var badge = document.createElement('span');
        badge.className = 'cmd-product-badge';
        badge.setAttribute('data-product', productKeys[b]);
        badge.innerHTML = JAMF_ICON_SVG + ' ' + (PRODUCT_LABELS[productKeys[b]] || productKeys[b]);
        badgeWrap.appendChild(badge);
      }
      header.appendChild(badgeWrap);
    }

    var commandsDiv = document.createElement('div');
    commandsDiv.className = 'group-commands';
    commandsDiv.style.display = expanded ? '' : 'none';

    // Store commands as data for lazy rendering
    commandsDiv._commands = group.commands;
    commandsDiv._rendered = false;

    if (expanded) {
      renderGroupCommands(commandsDiv);
    }

    header.addEventListener('click', function () {
      toggleGroup(header, commandsDiv);
    });
    header.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggleGroup(header, commandsDiv);
      }
    });

    container.appendChild(header);
    container.appendChild(commandsDiv);
    return container;
  }

  function renderGroupCommands(commandsDiv) {
    if (commandsDiv._rendered) return;
    var cmds = commandsDiv._commands;
    for (var i = 0; i < cmds.length; i++) {
      var pair = renderCommandRow(cmds[i]);
      commandsDiv.appendChild(pair.row);
      commandsDiv.appendChild(pair.detail);
    }
    commandsDiv._rendered = true;
  }

  function toggleGroup(header, commandsDiv) {
    var expanded = header.getAttribute('aria-expanded') === 'true';
    header.setAttribute('aria-expanded', String(!expanded));
    if (!expanded) {
      renderGroupCommands(commandsDiv);
      commandsDiv.style.display = '';
    } else {
      commandsDiv.style.display = 'none';
    }
  }

  function renderCommandRow(cmd) {
    var row = document.createElement('div');
    row.className = 'command-row';
    row.setAttribute('tabindex', '0');
    if (cmd.product) {
      row.setAttribute('data-product', cmd.product);
    }

    var nameSpan = document.createElement('span');
    nameSpan.className = 'command-name';
    var parts = cmd.command.split(' ');
    if (parts.length >= 2) {
      var prodSpan = document.createElement('span');
      prodSpan.className = 'cmd-product';
      highlightText(parts[0] + ' ', prodSpan);
      nameSpan.appendChild(prodSpan);
      if (parts.length > 2) {
        var resource = document.createElement('span');
        resource.className = 'cmd-prefix';
        highlightText(parts.slice(1, -1).join(' ') + ' ', resource);
        nameSpan.appendChild(resource);
      }
      var action = document.createElement('span');
      action.className = 'cmd-action';
      highlightText(parts[parts.length - 1], action);
      nameSpan.appendChild(action);
    } else {
      nameSpan.textContent = cmd.command;
    }
    nameSpan.setAttribute('data-copy', 'jamf-cli ' + cmd.command);
    var copyIcon = document.createElement('span');
    copyIcon.className = 'command-copy-icon';
    copyIcon.setAttribute('title', 'Copy: jamf-cli ' + cmd.command);
    copyIcon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    copyIcon.addEventListener('click', function (e) {
      e.stopPropagation();
      copyWithFeedback('jamf-cli ' + cmd.command, nameSpan);
    });
    nameSpan.appendChild(copyIcon);
    if (newCommandSet[cmd.command]) {
      var newBadge = document.createElement('span');
      newBadge.className = 'new-badge';
      newBadge.textContent = 'New';
      nameSpan.appendChild(newBadge);
    }
    row.appendChild(nameSpan);

    var descLine = document.createElement('div');
    descLine.className = 'command-desc-line';

    var descSpan = document.createElement('span');
    descSpan.className = 'command-desc';
    highlightText(cmd.description || '', descSpan);
    descLine.appendChild(descSpan);

    // Product badge on every command
    if (cmd.product && PRODUCT_LABELS[cmd.product]) {
      var prodBadge = document.createElement('span');
      prodBadge.className = 'cmd-product-badge';
      prodBadge.setAttribute('data-product', cmd.product);
      prodBadge.innerHTML = JAMF_ICON_SVG + ' ' + PRODUCT_LABELS[cmd.product]; // static SVG constant, safe
      descLine.appendChild(prodBadge);
    }

    var rowChevron = document.createElement('span');
    rowChevron.className = 'row-chevron';
    rowChevron.textContent = '\u203A';
    descLine.appendChild(rowChevron);

    // Cross-product reference badge
    var relatedProduct = detectRelatedProduct(cmd);
    if (relatedProduct) {
      var xrefBadge = document.createElement('span');
      xrefBadge.className = 'xref-badge';
      xrefBadge.setAttribute('data-product', relatedProduct);
      xrefBadge.innerHTML = JAMF_ICON_SVG + ' ' + (PRODUCT_LABELS[relatedProduct] || relatedProduct);
      descLine.appendChild(xrefBadge);
    }

    row.appendChild(descLine);

    var hasAliases = cmd.aliases && cmd.aliases.length > 0;
    var hasFlags = cmd.flags && cmd.flags.length > 0;

    if (hasAliases || hasFlags) {
      var meta = document.createElement('div');
      meta.className = 'command-meta';

      if (hasAliases) {
        for (var a = 0; a < cmd.aliases.length; a++) {
          var aliasBadge = document.createElement('span');
          aliasBadge.className = 'alias-badge';
          aliasBadge.textContent = 'alias: ' + cmd.aliases[a];
          meta.appendChild(aliasBadge);
        }
      }

      if (hasFlags) {
        for (var f = 0; f < cmd.flags.length; f++) {
          var pill = document.createElement('span');
          pill.className = 'flag-pill';
          pill.textContent = cmd.flags[f];
          meta.appendChild(pill);
        }
      }

      row.appendChild(meta);
    }

    // One-time expand hint below the command row content
    var hintEl = null;
    if (!expandHintShown) {
      expandHintShown = true;
      hintEl = document.createElement('div');
      hintEl.className = 'expand-hint';
      hintEl.textContent = '\u2193 Click any command to expand';
      row.appendChild(hintEl);
    }

    // Detail panel (hidden by default)
    var detail = document.createElement('div');
    detail.className = 'command-expanded';
    detail.appendChild(buildDetailContent(cmd));

    row.addEventListener('click', function () {
      var isOpen = detail.classList.contains('open');

      // Close any previously expanded command
      if (expandedCommand && expandedCommand !== detail) {
        expandedCommand.classList.remove('open');
        // Reset chevron on previously expanded row
        var prevRow = expandedCommand.previousElementSibling;
        if (prevRow) prevRow.classList.remove('expanded');
      }

      detail.classList.toggle('open');
      row.classList.toggle('expanded');
      expandedCommand = isOpen ? null : detail;

      // Remove the one-time hint after first click
      if (hintEl && hintEl.parentNode) {
        hintEl.parentNode.removeChild(hintEl);
        hintEl = null;
      }

      // Update URL hash for deep linking
      if (!isOpen) {
        var cmdHash = '#cmd/' + cmd.command.replace(/ /g, '/');
        history.pushState({ search: document.getElementById('search').value, cmd: cmd.command }, '', cmdHash);
      } else {
        history.pushState({ search: document.getElementById('search').value, cmd: null }, '', '#commands');
      }
    });

    return { row: row, detail: detail };
  }

  function copyWithFeedback(text, element) {
    navigator.clipboard.writeText(text).then(function () {
      element.classList.add('copied');
      setTimeout(function () { element.classList.remove('copied'); }, 1500);
    });
  }

  function createDetailHeading(text) {
    var el = document.createElement('div');
    el.className = 'detail-heading';
    el.textContent = text;
    return el;
  }

  function getParentPath(command) {
    var parts = command.split(' ');
    if (parts.length > 1) return parts.slice(0, -1).join(' ');
    return null;
  }

  // Pre-computed sibling lookup: parent path → [commands]
  var siblingMap = null;

  function buildSiblingMap() {
    siblingMap = {};
    for (var i = 0; i < allCommands.length; i++) {
      var parent = getParentPath(allCommands[i].command);
      if (!parent) continue;
      if (!siblingMap[parent]) siblingMap[parent] = [];
      siblingMap[parent].push(allCommands[i]);
    }
  }

  function findSiblings(cmd) {
    if (!siblingMap) buildSiblingMap();
    var parent = getParentPath(cmd.command);
    if (!parent || !siblingMap[parent]) return [];
    return siblingMap[parent].filter(function (c) { return c.command !== cmd.command; });
  }

  function buildDetailContent(cmd) {
    var frag = document.createDocumentFragment();

    // Breadcrumb: "Subcommand of pro computers"
    var parent = getParentPath(cmd.command);
    if (parent) {
      var breadcrumb = document.createElement('div');
      breadcrumb.className = 'detail-breadcrumb';
      breadcrumb.textContent = 'Subcommand of ';
      var parentLink = document.createElement('a');
      parentLink.className = 'detail-parent-link';
      parentLink.textContent = parent;
      parentLink.href = '#';
      parentLink.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        navigateToCommand(parent);
      });
      breadcrumb.appendChild(parentLink);
      frag.appendChild(breadcrumb);
    }

    // Hero example (if available)
    var heroKey = findHeroKey(cmd.command);
    if (heroKey) {
      var example = heroExamples[heroKey];
      var pre = document.createElement('pre');
      var promptLine = '$ ' + example.example + '\n';
      pre.textContent = promptLine + example.output;
      frag.appendChild(pre);
    }

    // Usage examples (from examples.json)
    var examples = commandExamples[cmd.command];
    if (examples && examples.length > 0) {
      frag.appendChild(createDetailHeading('Examples'));
      for (var ex = 0; ex < examples.length; ex++) {
        var exBlock = document.createElement('div');
        exBlock.className = 'example-block';
        if (examples[ex].description) {
          var exDesc = document.createElement('div');
          exDesc.className = 'example-desc';
          exDesc.textContent = examples[ex].description;
          exBlock.appendChild(exDesc);
        }
        var exPre = document.createElement('pre');
        exPre.className = 'example-command';
        exPre.textContent = '$ ' + examples[ex].command;
        exPre.title = 'Click to copy';
        (function (text, el) {
          el.addEventListener('click', function (e) {
            e.stopPropagation();
            navigator.clipboard.writeText(text).then(function () {
              el.classList.add('copied');
              setTimeout(function () { el.classList.remove('copied'); }, 1500);
            });
          });
        })(examples[ex].command, exPre);
        exBlock.appendChild(exPre);
        frag.appendChild(exBlock);
      }
    }

    // Related commands (siblings under same parent)
    var siblings = findSiblings(cmd);
    if (siblings.length > 0) {
      frag.appendChild(createDetailHeading('Related commands'));
      var siblingList = document.createElement('div');
      siblingList.className = 'detail-siblings';
      for (var s = 0; s < siblings.length; s++) {
        var sibCmd = siblings[s];
        var sibParts = sibCmd.command.split(' ');
        var sibAction = sibParts[sibParts.length - 1];

        var sibEl = document.createElement('a');
        sibEl.className = 'sibling-link';
        sibEl.href = '#';
        sibEl.setAttribute('data-product', sibCmd.product || '');
        sibEl.textContent = sibAction;
        sibEl.title = 'jamf-cli ' + sibCmd.command;
        (function (command) {
          sibEl.addEventListener('click', function (e) {
            e.preventDefault();
            e.stopPropagation();
            navigateToCommand(command);
          });
        })(sibCmd.command);
        siblingList.appendChild(sibEl);
      }
      frag.appendChild(siblingList);
    }

    return frag;
  }

  function findHeroKey(command) {
    // Strip the "jamf-cli " prefix if present in the command path,
    // then try matching against hero example keys.
    // Hero keys are like "pro overview", "protect analytics list"
    // Command paths from commands.json are like "pro overview", "pro computers list"
    var keys = Object.keys(heroExamples);
    for (var i = 0; i < keys.length; i++) {
      if (command === keys[i]) return keys[i];
      // Also try matching the command path without its leaf subcommand
      // e.g., "pro computers list" matches hero key "pro computers list"
      // and "pro comp erase" matches hero key "pro comp erase"
    }
    return null;
  }

  // ===== Search =====

  function setupSearch() {
    var search = document.getElementById('search');
    if (!search) return;
    var wrap = search.closest('.search-wrap');
    var clearBtn = document.getElementById('search-clear');

    var debounceTimer;
    search.addEventListener('input', function () {
      if (wrap) wrap.classList.toggle('has-value', search.value.length > 0);
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(function () {
        var query = search.value.trim();
        renderCatalog(allCommands, query, activeProduct);
      }, 200);
    });

    if (clearBtn) {
      clearBtn.addEventListener('click', function () {
        search.value = '';
        if (wrap) wrap.classList.remove('has-value');
        renderCatalog(allCommands, '', activeProduct);
        search.focus();
        history.pushState({ search: '', cmd: null }, '', '#commands');
      });
    }
  }

  // ===== Product Tabs =====

  function activateProductTab(filter) {
    var tabs = document.querySelectorAll('.tab');
    for (var j = 0; j < tabs.length; j++) {
      tabs[j].classList.remove('active');
      if (tabs[j].getAttribute('data-filter') === filter) {
        tabs[j].classList.add('active');
      }
    }
    activeProduct = filter;
    var search = document.getElementById('search');
    var query = search ? search.value.trim() : '';
    renderCatalog(allCommands, query, activeProduct);
  }

  function setupTabs() {
    var tabs = document.querySelectorAll('.tab');
    for (var i = 0; i < tabs.length; i++) {
      tabs[i].addEventListener('click', function () {
        activateProductTab(this.getAttribute('data-filter'));
      });
    }
  }

  function setupToggleAll() {
    var btn = document.getElementById('toggle-all');
    if (!btn) return;
    var allExpanded = false;

    btn.addEventListener('click', function () {
      allExpanded = !allExpanded;
      var headers = document.querySelectorAll('.catalog-group-header');
      for (var i = 0; i < headers.length; i++) {
        headers[i].setAttribute('aria-expanded', String(allExpanded));
        var commands = headers[i].nextElementSibling;
        if (commands && commands.classList.contains('group-commands')) {
          if (allExpanded) {
            renderGroupCommands(commands);
            commands.style.display = '';
          } else {
            commands.style.display = 'none';
          }
        }
      }
      btn.textContent = allExpanded ? 'Collapse All' : 'Expand All';
    });
  }

  function setupStatCards() {
    var cards = document.querySelectorAll('.stat-card[data-tab]');
    for (var i = 0; i < cards.length; i++) {
      cards[i].addEventListener('click', function () {
        activateProductTab(this.getAttribute('data-tab'));
      });
    }
  }

  // ===== Deep Linking & Navigation =====

  function navigateToCommand(command) {
    var search = document.getElementById('search');
    if (search) {
      var cmdHash = '#cmd/' + command.replace(/ /g, '/');
      history.pushState({ search: command, cmd: command }, '', cmdHash);
      search.value = command;
      search.dispatchEvent(new Event('input'));
      search.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  }

  function handleDeepLink() {
    var hash = window.location.hash;
    if (!hash || !hash.startsWith('#cmd/')) return;
    var command = hash.slice(5).replace(/\//g, ' ');
    var search = document.getElementById('search');
    if (search) {
      search.value = command;
      search.dispatchEvent(new Event('input'));
    }
  }

  function setupDeepLinking() {
    window.addEventListener('popstate', function (e) {
      var search = document.getElementById('search');
      if (!search) return;
      if (e.state && e.state.search != null) {
        search.value = e.state.search;
      } else {
        var hash = window.location.hash;
        if (hash && hash.startsWith('#cmd/')) {
          search.value = hash.slice(5).replace(/\//g, ' ');
        } else {
          search.value = '';
        }
      }
      search.dispatchEvent(new Event('input'));
    });
  }

  function setupNavScroll() {
    var hero = document.getElementById('hero');
    var nav = document.querySelector('nav');
    if (!hero || !nav || !('IntersectionObserver' in window)) return;

    var observer = new IntersectionObserver(function (entries) {
      if (entries[0].isIntersecting) {
        nav.classList.remove('scrolled');
      } else {
        nav.classList.add('scrolled');
      }
    }, { threshold: 0 });

    observer.observe(hero);
  }

  // ===== Copy Buttons =====

  function setupKeyboardNav() {
    var catalog = document.getElementById('catalog');
    if (!catalog) return;

    document.addEventListener('keydown', function (e) {
      // Only handle when not in an input
      var tag = document.activeElement.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;

      var rows = catalog.querySelectorAll('.command-row');
      if (rows.length === 0) return;

      var currentIndex = -1;
      for (var i = 0; i < rows.length; i++) {
        if (rows[i] === document.activeElement || rows[i].contains(document.activeElement)) {
          currentIndex = i;
          break;
        }
      }

      if (e.key === 'ArrowDown' || e.key === 'j') {
        e.preventDefault();
        var next = currentIndex + 1;
        if (next < rows.length) {
          rows[next].focus();
          rows[next].scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        }
      } else if (e.key === 'ArrowUp' || e.key === 'k') {
        e.preventDefault();
        var prev = currentIndex - 1;
        if (prev >= 0) {
          rows[prev].focus();
          rows[prev].scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        }
      } else if (e.key === 'Enter' && currentIndex >= 0) {
        e.preventDefault();
        rows[currentIndex].click();
      } else if (e.key === 'c' && currentIndex >= 0) {
        e.preventDefault();
        var nameEl = rows[currentIndex].querySelector('.command-name');
        if (nameEl) {
          var copyText = nameEl.getAttribute('data-copy');
          if (copyText) copyWithFeedback(copyText, nameEl);
        }
      }
    });
  }

  function setupCopyButtons() {
    document.addEventListener('click', function (e) {
      var btn = e.target.closest('.copy-btn');
      if (!btn) return;
      e.stopPropagation();
      var text = btn.getAttribute('data-copy');
      if (!text) return;
      navigator.clipboard.writeText(text).then(function () {
        btn.classList.add('copied');
        setTimeout(function () { btn.classList.remove('copied'); }, 2000);
      });
    });
  }

  // ===== Bootstrap =====

  document.addEventListener('DOMContentLoaded', init);
})();
