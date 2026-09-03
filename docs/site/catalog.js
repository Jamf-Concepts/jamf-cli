(function () {
  'use strict';

  // Pillars chunk the 28+ groups into 7 high-level sections so visitors can
  // form a mental map without memorising every group name. Order within each
  // pillar is preserved from GROUP_ORDER. Groups not listed here render
  // without a pillar divider (Protect-specific groups, etc.).
  var PILLARS = [
    { label: 'Quick Access',       groups: ['Getting Started', 'Power Commands', 'Configuration', 'Shell Completion', 'Utilities', 'Core Commands', 'Products'] },
    { label: 'Devices',            groups: ['Computer Management', 'Mobile Device Management', 'Enrollment', 'Inventory & Search', 'Organization'] },
    { label: 'Software & Content', groups: ['Apps & Patching', 'Distribution & JCDS', 'Scripts & Policies', 'Self Service', 'Jamf App Integrations'] },
    { label: 'Identity & Access',  groups: ['Users & Groups', 'Admin Accounts', 'Identity Providers', 'Admin SSO', 'API Access'] },
    { label: 'Infrastructure',     groups: ['MDM & Certificates', 'OS Updates', 'Security', 'Server Health', 'System Integrations'] },
    { label: 'Platform API',       groups: ['Platform - Configuration', 'Platform - Compliance', 'Platform - Devices & Users', 'Platform'] },
    { label: 'Classic API',        groups: ['Classic - Computers', 'Classic - Mobile Devices', 'Classic - Configuration', 'Classic - Administration', 'Classic - Patch Management'] }
  ];

  // Reverse-index built lazily on first use: { groupName: pillarLabel }
  var _pillarIndex = null;
  function pillarFor(groupName) {
    if (_pillarIndex === null) {
      _pillarIndex = {};
      for (var i = 0; i < PILLARS.length; i++) {
        for (var j = 0; j < PILLARS[i].groups.length; j++) {
          _pillarIndex[PILLARS[i].groups[j]] = PILLARS[i].label;
        }
      }
    }
    return _pillarIndex[groupName] || null;
  }

  var GROUP_ORDER = [
    'Getting Started',
    'Platform - Configuration',
    'Platform - Compliance',
    'Platform - Devices & Users',
    'Platform',
    'Configuration',
    'Shell Completion',
    'Utilities',
    'Power Commands',
    'Computer Management',
    'Mobile Device Management',
    'Enrollment',
    'Apps & Patching',
    'Distribution & JCDS',
    'Scripts & Policies',
    'Self Service',
    'Jamf App Integrations',
    'Inventory & Search',
    'Organization',
    'Users & Groups',
    'Admin Accounts',
    'Identity Providers',
    'Admin SSO',
    'API Access',
    'MDM & Certificates',
    'OS Updates',
    'Security',
    'Server Health',
    'System Integrations',
    'Classic - Computers',
    'Classic - Mobile Devices',
    'Classic - Configuration',
    'Classic - Administration',
    'Classic - Patch Management',
    'Security Configuration',
    'Endpoints',
    'Access & Identity',
    'Device Risk & Lifecycle',
    'Shared Signals & Events'
  ];


  var allCommands = [];
  var newCommandSet = {};
  var heroExamples = {};
  var commandExamples = {};
  var activeProduct = 'all';
  var expandedCommand = null;
  var currentSearchQuery = '';

  // The group shown in the table. null means "first group of the current
  // product". A search shows every matching group and ignores this.
  var selectedGroup = null;

  // The product the selected group was picked under. A group name repeats
  // across products ("Getting Started" exists under Platform and under Pro),
  // so the name alone marked two sidebar items active for one table.
  var selectedProduct = null;

  // Set while a popstate replay drives the catalog, so re-expanding a row
  // does not push the entry we are navigating back through.
  var suppressHashUpdate = false;

  // ===== Initialization =====

  function init() {
    loadHeroExamples();
    loadExamples();
    setupSearch();
    setupTabs();
    setupStatCards();
    setupToggleAll();
    setupKeyboardNav();
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
          catalog.innerHTML = '<p style="text-align:center;color:var(--font-secondary);padding:2rem 0;">Failed to load commands. ' +
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
    setText('nav-command-count', count.toLocaleString());
    setText('commands-total', count.toLocaleString());

    var search = document.getElementById('search');
    if (search) search.placeholder = 'Search ' + count.toLocaleString() + ' commands... (press / to focus)';

    var proCount = 0;
    var protectCount = 0;
    var schoolCount = 0;
    var securityCount = 0;
    var platformCount = 0;
    var productCount = 0;
    for (var i = 0; i < data.commands.length; i++) {
      var p = data.commands[i].product;
      var g = data.commands[i].group || '';
      if (p === 'pro') proCount++;
      else if (p === 'protect') protectCount++;
      else if (p === 'school') schoolCount++;
      else if (p === 'security') securityCount++;
      if (p === 'platform' || g.indexOf('Platform') === 0) platformCount++;
      if (p) productCount++;
    }
    var coreCount = count - productCount;
    animateCount('stat-pro', proCount);
    animateCount('stat-protect', protectCount);
    animateCount('stat-school', schoolCount);
    animateCount('stat-security', securityCount);
    animateCount('stat-platform', platformCount);
    animateCount('stat-core', coreCount);

    var versionBadge = document.getElementById('version-badge');
    if (versionBadge && version) {
      versionBadge.textContent = 'v' + version;
    }

    var lastUpdated = document.getElementById('last-updated');
    if (lastUpdated && generatedAt) {
      lastUpdated.textContent = ' · generated ' + formatDate(generatedAt);
    }

    var footerVersion = document.getElementById('footer-version');
    if (footerVersion && version) {
      footerVersion.textContent = 'jamf-cli v' + version;
    }
  }

  function animateCount(id, target) {
    var el = document.getElementById(id);
    if (!el) return;

    // Zero-state: a product with no commands in the deployed catalog (e.g. a
    // namespace merged after the last release the site builds from) reads
    // "Soon" rather than a stark "0", which reads as broken. Auto-clears to
    // the real number once that product ships in a release.
    if (target === 0) {
      el.textContent = 'Soon';
      el.classList.add('loaded');
      return;
    }

    var duration = 1200;
    var start = null;
    function step(ts) {
      if (!start) start = ts;
      var progress = Math.min((ts - start) / duration, 1);
      var eased = 1 - Math.pow(1 - progress, 3);
      el.textContent = Math.round(eased * target).toLocaleString();
      if (progress < 1) {
        requestAnimationFrame(step);
      } else {
        el.classList.add('loaded');
      }
    }
    requestAnimationFrame(step);
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

  // Split sorted groups by product. A group holding several products
  // becomes one entry per product, keyed by product; commands carrying no
  // product go to the shared list under the virtual "core" product.
  function splitByProduct(sorted) {
    var shared = [];
    var products = {};
    for (var i = 0; i < sorted.length; i++) {
      var byProduct = {};
      var noProd = [];
      for (var j = 0; j < sorted[i].commands.length; j++) {
        var c = sorted[i].commands[j];
        if (c.product) { (byProduct[c.product] = byProduct[c.product] || []).push(c); }
        else noProd.push(c);
      }
      if (noProd.length) shared.push({ name: sorted[i].name, commands: noProd });
      var keys = Object.keys(byProduct);
      for (var k = 0; k < keys.length; k++) {
        (products[keys[k]] = products[keys[k]] || []).push({ name: sorted[i].name, commands: byProduct[keys[k]] });
      }
    }
    return { shared: shared, products: products };
  }

  // Flatten a split into the render order: every product in PRODUCT_LABELS
  // order, then the shared groups.
  function orderedTables(split) {
    var out = [];
    var order = Object.keys(PRODUCT_LABELS);
    for (var i = 0; i < order.length; i++) {
      var groups = split.products[order[i]] || [];
      for (var g = 0; g < groups.length; g++) out.push({ group: groups[g], product: order[i] });
    }
    for (var sh = 0; sh < split.shared.length; sh++) {
      out.push({ group: split.shared[sh], product: 'core' });
    }
    return out;
  }

  function renderCatalog(commands, searchQuery, productFilter) {
    var catalog = document.getElementById('catalog');
    if (!catalog) return;

    currentSearchQuery = (searchQuery || '').trim().toLowerCase();
    expandedCommand = null;
    var hasSearch = !!currentSearchQuery;

    // Sidebar: full groups for the whole product filter, never narrowed by
    // search. With no search the table comes from this same split, so the
    // search-side pass only runs when there is a search to run it for.
    var nav = splitByProduct(sortGroups(groupCommands(filterCommands(commands, '', productFilter))));

    // Pick the table content.
    var tables = [];
    if (hasSearch) {
      tables = orderedTables(splitByProduct(sortGroups(groupCommands(filterCommands(commands, searchQuery, productFilter)))));
    } else {
      var all = orderedTables(nav);
      // Prefer the exact group-and-product pair, then the name alone (a
      // stale selectedProduct from another tab), then the first group.
      var pick = null;
      for (var a = 0; a < all.length; a++) {
        if (all[a].group.name === selectedGroup && all[a].product === selectedProduct) { pick = all[a]; break; }
      }
      if (!pick) {
        for (var b = 0; b < all.length; b++) {
          if (all[b].group.name === selectedGroup) { pick = all[b]; break; }
        }
      }
      if (!pick && all.length) { pick = all[0]; selectedGroup = pick.group.name; }
      if (pick) { selectedProduct = pick.product; tables.push(pick); }
    }

    renderGroupNav(nav.products, nav.shared, productFilter);

    catalog.innerHTML = '';
    if (tables.length === 0) {
      var empty = document.createElement('p');
      empty.className = 'catalog-empty';
      empty.textContent = 'No commands match your search.';
      catalog.appendChild(empty);
      return;
    }
    for (var t = 0; t < tables.length; t++) {
      catalog.appendChild(renderGroupTable(tables[t].group, tables[t].product, hasSearch));
    }
  }

  var PRODUCT_LABELS = {
    platform: 'Jamf Platform',
    pro: 'Jamf Pro',
    protect: 'Jamf Protect',
    school: 'Jamf School',
    security: 'Jamf Security Cloud'
    // connect: 'Jamf Connect'
  };

  // Sidebar: one heading per product, one item per group with a count.
  // A product not in the active filter collapses to its heading.
  function renderGroupNav(productGroups, sharedGroups, activeFilter) {
    var nav = document.getElementById('group-nav');
    if (!nav) return;
    nav.innerHTML = '';
    renderGroupSelect(productGroups, sharedGroups);

    var order = Object.keys(PRODUCT_LABELS).concat(['core']);
    for (var i = 0; i < order.length; i++) {
      var prod = order[i];
      var groups = prod === 'core' ? sharedGroups : (productGroups[prod] || []);
      if (groups.length === 0) continue;
      var total = 0;
      for (var g = 0; g < groups.length; g++) total += groups[g].commands.length;

      var head = document.createElement('div');
      head.className = 'group-nav-product';
      head.setAttribute('data-product', prod);
      var dot = document.createElement('span');
      dot.className = 'group-nav-dot';
      head.appendChild(dot);
      head.appendChild(document.createTextNode(prod === 'core' ? 'Core' : PRODUCT_LABELS[prod]));
      var totalEl = document.createElement('span');
      totalEl.className = 'group-nav-count';
      totalEl.textContent = total.toLocaleString();
      head.appendChild(totalEl);
      nav.appendChild(head);

      // The platform filter cross-cuts products: filterCommands admits Pro
      // and School commands whose group starts with "Platform", so those
      // headings have to open too. A collapsed heading is a count with no
      // selectable item under it, and the table-pick list can still select
      // one of its groups.
      var expanded = activeFilter === 'all' || activeFilter === 'platform' || activeFilter === prod;
      if (!expanded) continue;

      var lastPillar = null;
      for (var k = 0; k < groups.length; k++) {
        var pillar = prod === 'pro' ? pillarFor(groups[k].name) : null;
        if (pillar && pillar !== lastPillar) {
          var pl = document.createElement('div');
          pl.className = 'group-nav-pillar';
          pl.textContent = pillar;
          nav.appendChild(pl);
          lastPillar = pillar;
        }
        nav.appendChild(renderGroupNavItem(groups[k], prod));
      }
    }
  }

  // Below 1024px the sidebar is hidden and this select takes its place.
  // Both controls are always in the DOM; only CSS decides which one shows.
  function renderGroupSelect(productGroups, sharedGroups) {
    var select = document.getElementById('group-select');
    if (!select) return;
    select.innerHTML = '';
    var orderS = Object.keys(PRODUCT_LABELS).concat(['core']);
    for (var s = 0; s < orderS.length; s++) {
      var prod = orderS[s];
      var gs = prod === 'core' ? sharedGroups : (productGroups[prod] || []);
      if (!gs.length) continue;
      var og = document.createElement('optgroup');
      og.label = prod === 'core' ? 'Core' : PRODUCT_LABELS[prod];
      for (var t = 0; t < gs.length; t++) {
        var opt = document.createElement('option');
        opt.value = gs[t].name;
        opt.setAttribute('data-product', prod);
        opt.textContent = gs[t].name + ' (' + gs[t].commands.length + ')';
        if (gs[t].name === selectedGroup && prod === selectedProduct) opt.selected = true;
        og.appendChild(opt);
      }
      select.appendChild(og);
    }
  }

  function renderGroupNavItem(group, prod) {
    var item = document.createElement('button');
    item.type = 'button';
    item.className = 'group-nav-item';
    item.setAttribute('data-group', group.name);
    item.setAttribute('data-product', prod);
    if (group.name === selectedGroup && prod === selectedProduct) item.classList.add('active');
    var label = document.createElement('span');
    label.className = 'group-nav-label';
    label.textContent = group.name;
    item.appendChild(label);
    var count = document.createElement('span');
    count.className = 'group-nav-count';
    count.textContent = group.commands.length;
    item.appendChild(count);
    item.addEventListener('click', function () {
      selectedGroup = group.name;
      selectedProduct = prod;
      var search = document.getElementById('search');
      if (search && search.value) { search.value = ''; }
      renderCatalog(allCommands, '', activeProduct);
      var main = document.querySelector('.catalog-main');
      if (main) main.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
    return item;
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
        if (product === 'core') {
          // Core is a virtual product: everything without a namespace.
          return !cmd.product;
        }
        if (product === 'platform') {
          // Platform tab cross-cuts by group: every command in any
          // "Platform - …" or "Platform" group counts, regardless of
          // owning product (so Pro and School Platform commands surface
          // here too). Top-level platform setup/auth match by product.
          return cmd.product === 'platform' || (cmd.group && cmd.group.indexOf('Platform') === 0);
        }
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

  function renderGroupTable(group, product, showProduct) {
    var wrap = document.createElement('section');
    wrap.className = 'group-table';
    wrap.setAttribute('data-product', product);
    wrap.setAttribute('data-group', group.name);

    var head = document.createElement('div');
    head.className = 'group-table-head';
    var title = document.createElement('h3');
    title.textContent = group.name;
    head.appendChild(title);
    var meta = document.createElement('span');
    meta.className = 'group-table-meta';
    meta.textContent = group.commands.length + (group.commands.length === 1 ? ' command' : ' commands');
    head.appendChild(meta);
    if (showProduct) {
      var ptag = document.createElement('span');
      ptag.className = 'tag';
      ptag.setAttribute('data-product', product);
      ptag.textContent = product === 'core' ? 'Core' : PRODUCT_LABELS[product];
      head.appendChild(ptag);
    }
    wrap.appendChild(head);

    var table = document.createElement('div');
    table.className = 'cmd-table';
    table.setAttribute('role', 'table');
    var hdr = document.createElement('div');
    hdr.className = 'cmd-table-header';
    hdr.setAttribute('role', 'row');
    ['Command', 'Description', 'Marks'].forEach(function (h) {
      var cell = document.createElement('span');
      cell.setAttribute('role', 'columnheader');
      cell.textContent = h;
      hdr.appendChild(cell);
    });
    table.appendChild(hdr);
    for (var i = 0; i < group.commands.length; i++) {
      table.appendChild(renderCommandRow(group.commands[i]));
    }
    wrap.appendChild(table);
    return wrap;
  }

  function renderCommandRow(cmd) {
    var row = document.createElement('div');
    row.className = 'command-row';
    row.setAttribute('role', 'row');
    row.setAttribute('tabindex', '0');
    row.setAttribute('data-command', cmd.command);
    row.setAttribute('aria-expanded', 'false');
    if (cmd.product) row.setAttribute('data-product', cmd.product);

    var nameCell = document.createElement('span');
    nameCell.className = 'command-name';
    nameCell.setAttribute('role', 'cell');
    // The keyboard "c" shortcut reads this attribute off the focused row.
    nameCell.setAttribute('data-copy', 'jamf-cli ' + cmd.command);
    var parts = cmd.command.split(' ');
    if (parts.length >= 2) {
      var prodSpan = document.createElement('span');
      prodSpan.className = 'cmd-product';
      highlightText(parts[0] + ' ', prodSpan);
      nameCell.appendChild(prodSpan);
      if (parts.length > 2) {
        var mid = document.createElement('span');
        mid.className = 'cmd-prefix';
        highlightText(parts.slice(1, -1).join(' ') + ' ', mid);
        nameCell.appendChild(mid);
      }
      var action = document.createElement('span');
      action.className = 'cmd-action';
      highlightText(parts[parts.length - 1], action);
      nameCell.appendChild(action);
    } else {
      highlightText(cmd.command, nameCell);
    }
    var copyIcon = document.createElement('button');
    copyIcon.type = 'button';
    copyIcon.className = 'command-copy-icon';
    copyIcon.setAttribute('aria-label', 'Copy jamf-cli ' + cmd.command);
    copyIcon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    copyIcon.addEventListener('click', function (e) {
      e.stopPropagation();
      copyWithFeedback('jamf-cli ' + cmd.command, nameCell);
    });
    nameCell.appendChild(copyIcon);
    row.appendChild(nameCell);

    var desc = document.createElement('span');
    desc.className = 'command-desc';
    desc.setAttribute('role', 'cell');
    highlightText(cmd.description || '', desc);
    row.appendChild(desc);

    var marks = document.createElement('span');
    marks.className = 'command-marks';
    marks.setAttribute('role', 'cell');
    if (newCommandSet[cmd.command]) {
      var nb = document.createElement('span');
      nb.className = 'tag tag-new';
      nb.textContent = 'New';
      marks.appendChild(nb);
    }
    // Destructive mark — a fleet admin scanning the catalog should see at a
    // glance which commands can nuke a device. Ground truth is the CLI's own
    // --confirm-destructive flag; the verb fallback covers namespaces whose
    // destructive ops do not carry it (school/platform erase, security
    // purge). Plain `delete` is deliberately excluded: resource deletion is
    // routine, device destruction is not.
    if (isDestructiveCommand(cmd)) {
      var db = document.createElement('span');
      db.className = 'tag tag-danger';
      db.title = 'Destructive. Requires --confirm-destructive to run.';
      db.textContent = 'Destructive';
      marks.appendChild(db);
    }
    // Cross-product flow: `pro jamf-protect add-history-note` is a Pro
    // command that operates on Protect data, so the row carries the other
    // product's tag.
    var related = detectRelatedProduct(cmd);
    if (related && PRODUCT_LABELS[related]) {
      var xb = document.createElement('span');
      xb.className = 'tag';
      xb.setAttribute('data-product', related);
      xb.title = 'Operates on ' + PRODUCT_LABELS[related] + ' data';
      xb.textContent = PRODUCT_LABELS[related];
      marks.appendChild(xb);
    }
    row.appendChild(marks);

    // Expand on click, Enter or Space. The drawer is a sibling so the grid
    // row stays 3 cells.
    //
    // The document-level handler in setupKeyboardNav also acts on Enter, by
    // clicking the focused row. Both firing toggled the drawer twice — it
    // opened and shut in one keypress and pushed two history entries — so
    // this handler stops Enter and Space from reaching it. When the key
    // lands on a control inside the row (the copy button), the toggle is
    // skipped and the control's own activation runs alone.
    row.addEventListener('click', function () { toggleRowDetail(row, cmd); });
    row.addEventListener('keydown', function (e) {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.stopPropagation();
      if (e.target !== row) return;
      e.preventDefault();
      toggleRowDetail(row, cmd);
    });
    return row;
  }

  function toggleRowDetail(row, cmd) {
    var next = row.nextElementSibling;
    if (next && next.classList.contains('command-detail')) {
      next.remove();
      row.classList.remove('expanded');
      row.setAttribute('aria-expanded', 'false');
      if (expandedCommand === cmd.command) expandedCommand = null;
      updateCommandHash(null);
      return;
    }
    var table = row.parentNode;
    var open = table.querySelectorAll('.command-detail');
    for (var i = 0; i < open.length; i++) {
      open[i].previousElementSibling.classList.remove('expanded');
      open[i].previousElementSibling.setAttribute('aria-expanded', 'false');
      open[i].remove();
    }
    var detail = document.createElement('div');
    detail.className = 'command-detail';
    detail.setAttribute('role', 'row');
    detail.appendChild(buildDetailContent(cmd));
    row.parentNode.insertBefore(detail, row.nextSibling);
    row.classList.add('expanded');
    row.setAttribute('aria-expanded', 'true');
    expandedCommand = cmd.command;
    updateCommandHash(cmd.command);
  }

  // Keep the address bar in step with the open drawer so any row is a
  // shareable link. A popstate replay sets suppressHashUpdate, because
  // re-expanding the row would otherwise push the entry it navigated from.
  function updateCommandHash(command) {
    if (suppressHashUpdate) return;
    var search = document.getElementById('search');
    var query = search ? search.value : '';
    if (command) {
      history.pushState({ search: query, cmd: command }, '', '#cmd/' + command.replace(/ /g, '/'));
    } else {
      history.pushState({ search: query, cmd: null }, '', '#commands');
    }
  }

  // Destructive = the CLI's own --confirm-destructive flag (ground truth),
  // plus device-destructive verbs for namespaces that don't carry it.
  var DESTRUCTIVE_VERB_RE = /(^| )(erase|wipe|purge|unmanage|remove-mdm)( |$)/;

  function isDestructiveCommand(cmd) {
    if (cmd.flags && cmd.flags.indexOf('--confirm-destructive') !== -1) return true;
    return DESTRUCTIVE_VERB_RE.test(cmd.command);
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
    var grid = document.createElement('div');
    grid.className = 'detail-grid';
    var left = document.createElement('div');
    left.className = 'detail-col';
    var right = document.createElement('div');
    right.className = 'detail-col';

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
      left.appendChild(breadcrumb);
    }

    // Flags
    if (cmd.flags && cmd.flags.length > 0) {
      left.appendChild(createDetailHeading('Flags'));
      var flagWrap = document.createElement('div');
      flagWrap.className = 'detail-flags';
      for (var f = 0; f < cmd.flags.length; f++) {
        var pill = document.createElement('span');
        pill.className = 'tag tag-mono';
        if (cmd.flags[f] === '--confirm-destructive') pill.classList.add('tag-danger');
        pill.textContent = cmd.flags[f];
        flagWrap.appendChild(pill);
      }
      left.appendChild(flagWrap);
    }

    // Aliases
    if (cmd.aliases && cmd.aliases.length > 0) {
      var aliases = document.createElement('div');
      aliases.className = 'detail-meta';
      var aliasLabel = document.createElement('strong');
      aliasLabel.textContent = 'Aliases: ';
      aliases.appendChild(aliasLabel);
      aliases.appendChild(document.createTextNode(cmd.aliases.join(', ')));
      left.appendChild(aliases);
    }

    // Required privileges, when the catalog carries them.
    if (cmd.privileges && cmd.privileges.length > 0) {
      var privs = document.createElement('div');
      privs.className = 'detail-meta';
      var privLabel = document.createElement('strong');
      privLabel.textContent = 'Privileges: ';
      privs.appendChild(privLabel);
      privs.appendChild(document.createTextNode(cmd.privileges.join(', ')));
      left.appendChild(privs);
    }

    // Related commands (siblings under same parent)
    var siblings = findSiblings(cmd);
    if (siblings.length > 0) {
      left.appendChild(createDetailHeading('Related commands'));
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
      left.appendChild(siblingList);
    }

    // Hero example: the command plus the output it prints.
    var heroKey = findHeroKey(cmd.command);
    if (heroKey) {
      var hero = heroExamples[heroKey];
      right.appendChild(createDetailHeading('Example'));
      right.appendChild(buildExampleBlock(hero.example, null));
      var out = document.createElement('pre');
      out.className = 'detail-output';
      out.textContent = hero.output;
      right.appendChild(out);
    }

    // Usage examples (from examples.json)
    var examples = commandExamples[cmd.command];
    if (examples && examples.length > 0) {
      right.appendChild(createDetailHeading(heroKey ? 'More examples' : 'Examples'));
      for (var ex = 0; ex < examples.length; ex++) {
        right.appendChild(buildExampleBlock(examples[ex].command, examples[ex].description));
      }
    }

    grid.appendChild(left);
    grid.appendChild(right);
    frag.appendChild(grid);
    return frag;
  }

  // A copyable shell line. The copy button is the shared .copy-btn the
  // index.html IIFE decorates with a check icon.
  function buildExampleBlock(command, description) {
    var block = document.createElement('div');
    block.className = 'detail-example';
    if (description) {
      var desc = document.createElement('div');
      desc.className = 'detail-meta';
      desc.textContent = description;
      block.appendChild(desc);
    }
    var line = document.createElement('div');
    line.className = 'code-block';
    var code = document.createElement('code');
    var prompt = document.createElement('span');
    prompt.className = 'code-prompt';
    prompt.textContent = '$ ';
    code.appendChild(prompt);
    code.appendChild(document.createTextNode(command));
    line.appendChild(code);
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'btn btn-primary btn-small copy-btn';
    btn.setAttribute('data-copy', command);
    btn.setAttribute('aria-label', 'Copy ' + command);
    btn.textContent = 'Copy';
    line.appendChild(btn);
    block.appendChild(line);
    return block;
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

    var navSearch = document.querySelector('.nav-search');
    if (navSearch) {
      navSearch.addEventListener('click', function () {
        if (window.openCommandPalette) window.openCommandPalette();
        else { var s = document.getElementById('search'); if (s) s.focus(); }
      });
    }

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
    selectedGroup = null; // first group of the new product
    selectedProduct = null;
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

    var gsel = document.getElementById('group-select');
    if (gsel) gsel.addEventListener('change', function () {
      selectedGroup = this.value;
      var chosen = this.options[this.selectedIndex];
      selectedProduct = chosen ? chosen.getAttribute('data-product') : null;
      var search = document.getElementById('search');
      if (search) search.value = '';
      renderCatalog(allCommands, '', activeProduct);
    });
  }

  function setupToggleAll() {
    var btn = document.getElementById('toggle-all');
    if (!btn) return;
    var allExpanded = false;

    btn.addEventListener('click', function () {
      allExpanded = !allExpanded;
      var catalog = document.getElementById('catalog');
      if (catalog) {
        var rows = catalog.querySelectorAll('.command-row');
        for (var i = 0; i < rows.length; i++) {
          var open = rows[i].nextElementSibling;
          var isOpen = !!(open && open.classList.contains('command-detail'));
          if (isOpen === allExpanded) continue;
          expandRow(rows[i], allExpanded);
        }
      }
      btn.textContent = allExpanded ? 'Collapse all' : 'Expand all';
    });
  }

  // Open or close one row's drawer without touching its siblings, so
  // "Expand all" can hold every drawer open at once. toggleRowDetail keeps
  // the one-at-a-time rule for a click.
  function expandRow(row, open) {
    var cmd = commandByPath(row.getAttribute('data-command'));
    if (!cmd) return;
    var next = row.nextElementSibling;
    var isOpen = !!(next && next.classList.contains('command-detail'));
    if (open === isOpen) return;
    if (!open) {
      next.remove();
      row.classList.remove('expanded');
      row.setAttribute('aria-expanded', 'false');
      if (expandedCommand === cmd.command) expandedCommand = null;
      return;
    }
    var detail = document.createElement('div');
    detail.className = 'command-detail';
    detail.setAttribute('role', 'row');
    detail.appendChild(buildDetailContent(cmd));
    row.parentNode.insertBefore(detail, row.nextSibling);
    row.classList.add('expanded');
    row.setAttribute('aria-expanded', 'true');
  }

  function commandByPath(command) {
    for (var i = 0; i < allCommands.length; i++) {
      if (allCommands[i].command === command) return allCommands[i];
    }
    return null;
  }

  function setupStatCards() {
    var links = document.querySelectorAll('#hero [data-tab]');
    for (var i = 0; i < links.length; i++) {
      links[i].addEventListener('click', function () {
        activateProductTab(this.getAttribute('data-tab'));
      });
    }
  }

  // ===== Deep Linking & Navigation =====

  function navigateToCommand(command) {
    var cmd = commandByPath(command);
    if (!cmd) return;
    selectedGroup = cmd.group;
    selectedProduct = cmd.product || 'core';
    var search = document.getElementById('search');
    if (search) search.value = '';
    renderCatalog(allCommands, '', activeProduct);
    var row = document.querySelector('.command-row[data-command="' + command.replace(/"/g, '\\"') + '"]');
    if (row) {
      toggleRowDetail(row, cmd);
      row.scrollIntoView({ behavior: 'smooth', block: 'center' });
      row.focus();
    }
  }

  function handleDeepLink() {
    var hash = window.location.hash;
    if (!hash || hash.indexOf('#cmd/') !== 0) return;
    navigateToCommand(hash.slice(5).replace(/\//g, ' '));
  }

  function setupDeepLinking() {
    window.addEventListener('popstate', function (e) {
      var hash = window.location.hash;
      var command = null;
      if (e.state && e.state.cmd) command = e.state.cmd;
      else if (hash && hash.indexOf('#cmd/') === 0) command = hash.slice(5).replace(/\//g, ' ');

      suppressHashUpdate = true;
      if (command) {
        navigateToCommand(command);
      } else {
        var search = document.getElementById('search');
        if (search) {
          search.value = (e.state && e.state.search) || '';
          renderCatalog(allCommands, search.value.trim(), activeProduct);
        }
      }
      suppressHashUpdate = false;
    });
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
