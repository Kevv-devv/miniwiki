(function(){
  function qs(sel, root){ return (root || document).querySelector(sel); }
  function qsa(sel, root){ return Array.from((root || document).querySelectorAll(sel)); }

  var savedTheme = localStorage.getItem('miniwiki-theme');
  if(savedTheme){ document.documentElement.setAttribute('data-theme', savedTheme); }
  qs('[data-theme-toggle]')?.addEventListener('click', function(){
    var cur = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    if(cur === 'light'){ document.documentElement.removeAttribute('data-theme'); localStorage.setItem('miniwiki-theme','light'); }
    else { document.documentElement.setAttribute('data-theme','dark'); localStorage.setItem('miniwiki-theme','dark'); }
  });
  qs('[data-nav-toggle]')?.addEventListener('click', function(){ qs('[data-nav-menu]')?.classList.toggle('open'); });

  function slugify(s){ return (s || '').toLowerCase().trim().replace(/[^a-z0-9\s-]/g,'').replace(/\s+/g,'-').replace(/-+/g,'-') || 'section'; }
  var body = qs('[data-wiki-body]');
  var toc = qs('[data-toc]');
  var tocList = qs('[data-toc-list]');
  if(body && toc && tocList){
    var heads = qsa('h2,h3,h4', body).slice(0, 40);
    if(heads.length){
      heads.forEach(function(h, i){
        if(!h.id){ h.id = slugify(h.textContent) + '-' + i; }
        var a = document.createElement('a');
        a.href = '#' + h.id;
        a.textContent = h.textContent;
        a.className = 'toc-level-' + h.tagName.substring(1);
        tocList.appendChild(a);
      });
      toc.hidden = false;
    }
  }

  var editor = qs('[data-editor]');
  var preview = qs('[data-live-preview]');
  var wordCount = qs('[data-word-count]');
  var charCount = qs('[data-char-count]');
  function escapeHTML(s){ return s.replace(/[&<>]/g, function(c){ return {'&':'&amp;','<':'&lt;','>':'&gt;'}[c]; }); }
  function inlineMarkdown(s){
    s = escapeHTML(s);
    s = s.replace(/`([^`]+)`/g, '<code>$1</code>');
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    s = s.replace(/\[\[([^\]|]+)\|([^\]]+)\]\]/g, '<a href="#">$2</a>');
    s = s.replace(/\[\[([^\]]+)\]\]/g, '<a href="#">$1</a>');
    s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2">$1</a>');
    return s;
  }
  function renderApprox(md){
    var lines = md.split(/\r?\n/), out = [], inList = false, inCode = false, code = [];
    function closeList(){ if(inList){ out.push('</ul>'); inList = false; } }
    lines.forEach(function(line){
      if(line.trim().startsWith('```')){ if(inCode){ out.push('<pre><code>'+escapeHTML(code.join('\n'))+'</code></pre>'); code=[]; inCode=false; } else { closeList(); inCode=true; } return; }
      if(inCode){ code.push(line); return; }
      if(!line.trim()){ closeList(); return; }
      var h = line.match(/^(#{1,4})\s+(.+)$/); if(h){ closeList(); out.push('<h'+h[1].length+'>'+inlineMarkdown(h[2])+'</h'+h[1].length+'>'); return; }
      var li = line.match(/^[-*]\s+(.+)$/); if(li){ if(!inList){ out.push('<ul>'); inList=true; } out.push('<li>'+inlineMarkdown(li[1])+'</li>'); return; }
      var bq = line.match(/^>\s?(.+)$/); if(bq){ closeList(); out.push('<blockquote>'+inlineMarkdown(bq[1])+'</blockquote>'); return; }
      closeList(); out.push('<p>'+inlineMarkdown(line)+'</p>');
    });
    closeList(); if(inCode){ out.push('<pre><code>'+escapeHTML(code.join('\n'))+'</code></pre>'); }
    return out.join('\n');
  }
  function updatePreview(){
    if(!editor) return;
    var text = editor.value || '';
    if(preview) preview.innerHTML = renderApprox(text);
    if(charCount) charCount.textContent = text.length;
    if(wordCount) wordCount.textContent = (text.trim().match(/\S+/g) || []).length;
  }
  function wrapSelection(before, after, placeholder){
    if(!editor) return;
    var start = editor.selectionStart, end = editor.selectionEnd;
    var selected = editor.value.slice(start, end) || placeholder;
    var insert = before + selected + after;
    editor.setRangeText(insert, start, end, 'select');
    editor.dispatchEvent(new Event('input'));
    editor.focus();
  }
  qsa('[data-md]').forEach(function(btn){
    btn.addEventListener('click', function(){
      var t = btn.getAttribute('data-md');
      if(t === 'bold') wrapSelection('**','**','bold text');
      if(t === 'italic') wrapSelection('*','*','italic text');
      if(t === 'heading') wrapSelection('\n## ','','Heading');
      if(t === 'link') wrapSelection('[','](https://example.com)','link text');
      if(t === 'image') wrapSelection('![','](/uploads/file-name.png)','image alt text');
      if(t === 'wikilink') wrapSelection('[[',']]','Page Name');
      if(t === 'category') wrapSelection('\n[[Category:',']]','Category Name');
      if(t === 'quote') wrapSelection('\n> ','','quoted text');
      if(t === 'code') wrapSelection('\n```\n','\n```\n','code here');
    });
  });
  editor?.addEventListener('input', updatePreview);
  updatePreview();
})();

(function(){
  function qs(sel, root){ return (root || document).querySelector(sel); }
  var drop = qs('[data-drop-zone]');
  var input = qs('[data-file-input]');
  var name = qs('[data-drop-name]');
  if(drop && input){
    ['dragenter','dragover'].forEach(function(ev){ drop.addEventListener(ev, function(e){ e.preventDefault(); drop.classList.add('dragover'); }); });
    ['dragleave','drop'].forEach(function(ev){ drop.addEventListener(ev, function(e){ e.preventDefault(); drop.classList.remove('dragover'); }); });
    drop.addEventListener('drop', function(e){
      if(e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length){
        input.files = e.dataTransfer.files;
        if(name) name.textContent = e.dataTransfer.files[0].name;
      }
    });
    input.addEventListener('change', function(){ if(name && input.files && input.files.length) name.textContent = input.files[0].name; });
  }

  document.addEventListener('keydown', function(e){
    var tag = (e.target && e.target.tagName || '').toLowerCase();
    var editingText = tag === 'textarea' || tag === 'input' || e.target.isContentEditable;
    if(editingText){ return; }
    if(e.key === 'e' || e.key === 'E'){
      var edit = document.querySelector('.page-tabs a[href$="/edit"]');
      if(edit){ window.location.href = edit.href; }
    }
    if(e.key === '/'){
      var search = document.querySelector('form.search input[name="q"]');
      if(!search){ window.location.href = '/search'; return; }
      e.preventDefault(); search.focus();
    }
    if(e.key === 'r' || e.key === 'R'){
      var recent = document.querySelector('a[href="/recent"]');
      if(recent){ window.location.href = recent.href; }
    }
  });
})();
