(function(){
  var root=document.documentElement;
  var toggle=document.querySelector(".theme-toggle");
  function current(){
    if(root.dataset.theme) return root.dataset.theme;
    return window.matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light";
  }
  if(toggle){
    toggle.addEventListener("click",function(){
      var next=current()==="dark"?"light":"dark";
      root.dataset.theme=next;
      try{localStorage.setItem("theme",next);}catch(e){}
    });
  }

  document.querySelectorAll(".tree .caret").forEach(function(btn){
    btn.addEventListener("click",function(){
      var li=btn.parentElement;
      var open=li.classList.toggle("open");
      btn.setAttribute("aria-expanded",open?"true":"false");
    });
  });

  var menu=document.querySelector(".menu-toggle");
  if(menu){menu.addEventListener("click",function(){document.body.classList.toggle("nav-open");});}

  var tocLinks=[].slice.call(document.querySelectorAll(".toc-body a"));
  if(tocLinks.length&&"IntersectionObserver" in window){
    var map={};
    tocLinks.forEach(function(a){map[decodeURIComponent(a.hash.slice(1))]=a;});
    var heads=[].slice.call(document.querySelectorAll(".prose h2[id],.prose h3[id]"));
    var obs=new IntersectionObserver(function(entries){
      entries.forEach(function(en){
        if(en.isIntersecting){
          tocLinks.forEach(function(a){a.classList.remove("on");});
          var a=map[en.target.id]; if(a) a.classList.add("on");
        }
      });
    },{rootMargin:"-80px 0px -70% 0px"});
    heads.forEach(function(h){obs.observe(h);});
  }

  document.querySelectorAll(".code").forEach(function(box){
    var btn=box.querySelector(".copy"),pre=box.querySelector("pre");
    if(!btn||!pre) return;
    btn.addEventListener("click",function(){
      var code=pre.querySelector("code")||pre;
      navigator.clipboard.writeText(code.innerText.replace(/\n$/,"")).then(function(){
        btn.textContent="Copied";setTimeout(function(){btn.textContent="Copy";},1500);
      });
    });
  });

  var script=document.currentScript||document.querySelector("script[data-pagefind]");
  var pfUrl=script&&script.dataset.pagefind;
  var mac=/Mac|iPhone|iPad/.test(navigator.platform||navigator.userAgent);
  document.querySelectorAll("[data-kbd]").forEach(function(k){k.textContent=mac?"⌘ K":"Ctrl K";});

  if(pfUrl&&/[?&]highlight=/.test(location.search)){
    import(pfUrl.replace("pagefind.js","pagefind-highlight.js")).then(function(){
      new window.PagefindHighlight({highlightParam:"highlight",markOptions:{className:"pf-mark"}});
    }).catch(function(){});
  }

  var modal=document.getElementById("search");
  if(modal){
    var input=modal.querySelector("input"),list=modal.querySelector(".sm-list");
    var pf=null,pfLoad=null,sel=-1,last=null,opener=null;
    var hint=list.innerHTML;
    function engine(){
      if(!pfLoad){
        pfLoad=import(pfUrl).then(function(m){
          pf=m;
          return m.options({highlightParam:"highlight",excerptLength:22}).then(function(){m.init();return m;});
        });
      }
      return pfLoad;
    }
    function esc(s){return String(s).replace(/[&<>"]/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;"}[c];});}
    function open(){
      opener=document.activeElement;
      modal.hidden=false;modal.classList.add("open");
      document.documentElement.style.overflow="hidden";
      input.value=input.value||"";input.focus();input.select();
      engine().catch(function(){list.innerHTML='<p class="sm-empty">Search is built when the site deploys. Run Pagefind after Hugo to try it locally.</p>';});
    }
    function close(){
      modal.classList.remove("open");modal.hidden=true;
      document.documentElement.style.overflow="";
      if(opener&&opener.focus) opener.focus();
    }
    function items(){return [].slice.call(list.querySelectorAll("a"));}
    function mark(i){
      var all=items(); if(!all.length) return;
      sel=(i+all.length)%all.length;
      all.forEach(function(a,j){a.classList.toggle("sel",j===sel);a.setAttribute("aria-selected",j===sel?"true":"false");});
      all[sel].scrollIntoView({block:"nearest"});
    }
    function render(q,pages){
      if(!pages.length){list.innerHTML='<p class="sm-empty">Nothing matches "'+esc(q)+'". Try a component name like Cilium or OpenBao.</p>';return;}
      list.innerHTML=pages.map(function(p){
        var subs=(p.sub_results||[]).filter(function(s){return s.url.indexOf("#")>0;}).slice(0,3);
        var crumb=p.meta.section?'<span class="sm-crumb">'+esc(p.meta.section)+'</span>':"";
        return '<div class="sm-group"><a class="sm-page" role="option" href="'+p.url+'">'+crumb+'<b>'+esc(p.meta.title||p.url)+'</b><span class="sm-ex">'+p.excerpt+'</span></a>'+
          subs.map(function(s){return '<a class="sm-sub" role="option" href="'+s.url+'"><b>'+esc(s.title)+'</b><span class="sm-ex">'+s.excerpt+'</span></a>';}).join("")+'</div>';
      }).join("");
      sel=-1;mark(0);
    }
    input.addEventListener("input",function(){
      var q=input.value.trim();
      if(!q){list.innerHTML=hint;sel=-1;return;}
      engine().then(function(m){return m.debouncedSearch(q,{},120);}).then(function(r){
        if(r===null) return;
        last=q;
        return Promise.all(r.results.slice(0,8).map(function(x){return x.data();})).then(function(pages){if(last===q) render(q,pages);});
      }).catch(function(){});
    });
    input.addEventListener("keydown",function(e){
      if(e.key==="ArrowDown"){e.preventDefault();mark(sel+1);}
      else if(e.key==="ArrowUp"){e.preventDefault();mark(sel-1);}
      else if(e.key==="Enter"){var a=items()[sel];if(a){e.preventDefault();location.href=a.getAttribute("href");}}
    });
    modal.addEventListener("keydown",function(e){
      if(e.key==="Escape"){e.preventDefault();close();}
      if(e.key==="Tab"){e.preventDefault();input.focus();}
    });
    modal.addEventListener("mousedown",function(e){if(e.target===modal) close();});
    modal.querySelector("[data-search-close]").addEventListener("click",close);
    document.querySelectorAll("[data-search-open]").forEach(function(b){b.addEventListener("click",open);});
    document.addEventListener("keydown",function(e){
      var typing=/input|textarea|select/i.test(document.activeElement.tagName);
      if((e.key==="k"||e.key==="K")&&(e.metaKey||e.ctrlKey)){e.preventDefault();modal.hidden?open():close();}
      else if(e.key==="/"&&!typing&&modal.hidden){e.preventDefault();open();}
    });
  }
})();
(function(){
  var nums=[].slice.call(document.querySelectorAll(".sts .n"));
  if(!nums.length||window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  nums.forEach(function(el,k){
    var m=String(el.dataset.v).match(/^(\d+)(.*)$/);
    if(!m) return;
    var target=+m[1],rest=m[2],t0=null,dur=1100;
    el.textContent="0"+rest;
    setTimeout(function(){requestAnimationFrame(function f(ts){
      if(!t0) t0=ts;
      var p=Math.min(1,(ts-t0)/dur);p=1-Math.pow(1-p,3);
      el.textContent=Math.round(target*p).toLocaleString("en-US")+rest;
      if(p<1) requestAnimationFrame(f);
    });},200+k*70);
  });
})();
