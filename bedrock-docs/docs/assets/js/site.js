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

  document.querySelectorAll(".prose pre").forEach(function(pre){
    var btn=document.createElement("button");
    btn.type="button";btn.className="copy";btn.textContent="Copy";
    btn.addEventListener("click",function(){
      var code=pre.querySelector("code")||pre;
      navigator.clipboard.writeText(code.innerText.replace(/\n$/,"")).then(function(){
        btn.textContent="Copied";setTimeout(function(){btn.textContent="Copy";},1500);
      });
    });
    pre.appendChild(btn);
  });

  var script=document.currentScript||document.querySelector("script[data-index]");
  var indexUrl=script&&script.dataset.index;
  var docs=null,loading=null;
  function load(){
    if(docs) return Promise.resolve(docs);
    if(!loading){
      loading=fetch(indexUrl).then(function(r){return r.json();}).then(function(d){docs=d;return d;}).catch(function(){docs=[];return docs;});
    }
    return loading;
  }
  function esc(s){return String(s).replace(/[&<>"]/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;"}[c];});}
  function highlight(text,terms){
    var out=esc(text);
    terms.forEach(function(t){
      if(t.length<2) return;
      out=out.replace(new RegExp("("+t.replace(/[.*+?^${}()|[\]\\]/g,"\\$&")+")","ig"),"<mark>$1</mark>");
    });
    return out;
  }
  function snippet(text,terms){
    var low=text.toLowerCase(),pos=-1;
    terms.some(function(t){pos=low.indexOf(t);return pos>=0;});
    if(pos<0) return text.slice(0,140);
    var start=Math.max(0,pos-50);
    return (start>0?"...":"")+text.slice(start,start+150)+"...";
  }
  function search(q){
    var terms=q.toLowerCase().split(/\s+/).filter(Boolean);
    if(!terms.length) return [];
    return docs.map(function(d){
      var title=d.title.toLowerCase(),desc=(d.description||"").toLowerCase(),body=(d.content||"").toLowerCase();
      var score=0;
      for(var i=0;i<terms.length;i++){
        var t=terms[i],hit=false;
        if(title.indexOf(t)>=0){score+=10;hit=true;}
        if(desc.indexOf(t)>=0){score+=4;hit=true;}
        var n=body.split(t).length-1;
        if(n){score+=Math.min(n,6);hit=true;}
        if(!hit) return null;
      }
      return {d:d,score:score};
    }).filter(Boolean).sort(function(a,b){return b.score-a.score;}).slice(0,8);
  }
  document.querySelectorAll("[data-search]").forEach(function(input){
    var box=input.parentElement.querySelector(".search-results");
    var sel=-1;
    function render(){
      var q=input.value.trim();
      if(!q){box.hidden=true;return;}
      load().then(function(){
        var res=search(q),terms=q.toLowerCase().split(/\s+/);
        sel=-1;
        if(!res.length){box.innerHTML='<div class="empty">No results for "'+esc(q)+'"</div>';}
        else{
          box.innerHTML=res.map(function(r){
            return '<a href="'+r.d.url+'"><span class="r-sec">'+esc(r.d.section||"")+'</span><span class="r-title">'+highlight(r.d.title,terms)+'</span><span class="r-snip">'+highlight(snippet(r.d.content||"",terms),terms)+'</span></a>';
          }).join("");
        }
        box.hidden=false;
      });
    }
    input.addEventListener("focus",load);
    input.addEventListener("input",render);
    input.addEventListener("keydown",function(e){
      var items=box.querySelectorAll("a");
      if(e.key==="ArrowDown"||e.key==="ArrowUp"){
        if(!items.length) return;
        e.preventDefault();
        sel=(sel+(e.key==="ArrowDown"?1:-1)+items.length)%items.length;
        items.forEach(function(a,i){a.classList.toggle("sel",i===sel);});
      }else if(e.key==="Enter"){
        var a=items[sel>=0?sel:0]; if(a) window.location.href=a.getAttribute("href");
      }else if(e.key==="Escape"){box.hidden=true;input.blur();}
    });
    document.addEventListener("click",function(e){if(!input.parentElement.contains(e.target)) box.hidden=true;});
  });
  document.addEventListener("keydown",function(e){
    if(e.key==="/"&&!/input|textarea/i.test(document.activeElement.tagName)){
      var i=document.querySelector(window.innerWidth>860?".search-top input":".search-side input");
      if(i){e.preventDefault();i.focus();}
    }
  });
})();
