// The calculation code of prototype/pricedin.html, copied verbatim.
//
// Each function body is the prototype's own text, with a comment that cites
// its lines. The prototype reads globals (S, Q, L, ttm, qyoy, tyoy, RECENT,
// WTD, STK, FIN). Here they come from the parameters of the enclosing
// function instead; nothing else changes. Code that only builds SVG or HTML
// is left out, and where a numeric expression sits inside such code, only
// the numeric lines are copied, unchanged, into a function of their own.
//
// Do not "fix" anything in this file. v1 corrections live in oracle.mjs.

/* eslint-disable */

// Lines 733-741 of loadData: the per-quarter series and the baselines.
// Q is the prototype's quarter table: [label, eps, close] per quarter.
export function loadData(Q) {
  let L, ttm, qyoy, tyoy, RECENT, WTD, LT, PEMED;
  L=Q.length-1;
  ttm=Q.map((q,i)=>i>=3?Q[i][1]+Q[i-1][1]+Q[i-2][1]+Q[i-3][1]:null);
  qyoy=Q.map((q,i)=>i>=4&&Q[i-4][1]>0?Q[i][1]/Q[i-4][1]-1:null);
  tyoy=ttm.map((t,i)=>i>=7&&ttm[i-4]>0?t/ttm[i-4]-1:null);
  RECENT=qyoy[L]; // today: the latest quarter against the same quarter a year earlier
  /* weighted trend: YoY growth of the last 8 quarters, weight halves every 2 quarters back */
  { let sw=0,sv=0; for(let k=0;k<8;k++){const v=qyoy[L-k]; if(v==null||!isFinite(v)) continue; const w=Math.pow(0.5,k/2); sw+=w; sv+=w*v;} WTD=sw?sv/sw:RECENT; }
  LT=ttm[L-12]>0&&ttm[L]>0?Math.pow(ttm[L]/ttm[L-12],1/3)-1:NaN;
  const pes=[];for(let i=6;i<=L;i++) if(Q[i][2]&&ttm[i]>0) pes.push(Q[i][2]/ttm[i]);
  pes.sort((a,b)=>a-b); PEMED=pes[Math.floor(pes.length/2)];
  return { L, ttm, qyoy, tyoy, RECENT, WTD, LT, PEMED };
}

// The math section (lines 767-838), the formatters it uses (840-842), tone
// (847) and defaultScen (914-917). S is the prototype's lever state, with
// rates in percent.
export function makeMath({ S, Q, L, ttm, qyoy, WTD, RECENT }) {
/* ---------- math ---------- */
const req=(P,E,r,n,pe,D)=>{const t=P*Math.pow(1+r,n)-D*n; if(!(E>0)||!(pe>0)) return NaN; if(t<=0) return -1; return Math.pow(t/(E*pe),1/n)-1;};
const proj=(P,E,g,n,pe,D)=>{const px=E*Math.pow(1+g,n)*pe; const tot=px+D*n; return {px, cum:tot/P-1, cagr:Math.pow(Math.max(tot,0)/P,1/n)-1};};
const reqPe=(P,E,r,n,g,D)=>(P*Math.pow(1+r,n)-D*n)/(E*Math.pow(1+g,n));
/* weighted trend as it stood at quarter t */
function wtdAt(t){let sw=0,sv=0; for(let k=0;k<8;k++){const v=qyoy[t-k]; if(v==null||!isFinite(v)) continue; const w=Math.pow(0.5,k/2); sw+=w; sv+=w*v;} return sw?sv/sw:NaN;}
/* trajectory: last year's change in the weighted trend continues, halving each year,
   never above the highest reading of the last two years, never below -50% */
function trajInfo(n){
  const past=[]; for(let k=0;k<8;k++){const v=wtdAt(L-k); if(isFinite(v)) past.push(v);}
  const prev=wtdAt(L-4), slope=isFinite(prev)?WTD-prev:0;
  const hi=Math.max(...past), lo=-0.5;
  let g=WTD, st=slope; const rates=[];
  for(let y=0;y<n;y++){st*=0.5; g=Math.min(hi,Math.max(lo,g+st)); rates.push(Math.min(Math.floor(hi*200)/2,Math.round(g*200)/2));}
  return {rates,slope,prev,hi,capped:slope>0&&rates.some(v=>Math.abs(v/100-hi)<0.006)};
}
function yearRates(){ // one rate per year of the holding period, in %
  if(!Array.isArray(S.ygr)) S.ygr=[];
  if(S.ygrSrc==='traj'&&S.ygr.length<S.n){const t=trajInfo(S.n).rates; while(S.ygr.length<S.n) S.ygr.push(t[S.ygr.length]);}
  while(S.ygr.length<S.n) S.ygr.push(S.ygr.length?S.ygr[S.ygr.length-1]:S.custom);
  return S.ygr.slice(0,S.n);
}
function yearlyAvg(){const r=yearRates(); const prod=r.reduce((a,v)=>a*(1+v/100),1); return prod>0?Math.pow(prod,1/r.length)-1:-1;}
function trlMonths(){let m=Math.round((+S.trl||12)/3)*3; return Math.min(45,Math.max(6,m));}
/* average yearly growth of TTM EPS over the chosen number of months */
function trlGrowth(){const m=trlMonths(), q=m/3, a=ttm[L-q], b=ttm[L]; return a>0&&b>0?Math.pow(b/a,12/m)-1:NaN;}
const base=()=>S.mode==='wtd'?WTD:S.mode==='today'?RECENT:S.mode==='trl'?trlGrowth():(S.yearly?yearlyAvg():S.custom/100);
/* EPS on the baseline path after k quarters; follows the yearly rates when they are set */
function basePath(E,g,k){
  if(!(S.mode==='custom'&&S.yearly)) return E*Math.pow(1+g,k/4);
  const r=yearRates(); let e=E; const full=Math.floor(k/4);
  for(let y=0;y<full&&y<r.length;y++) e*=1+r[y]/100;
  const part=k/4-full; if(part>0&&full<r.length) e*=Math.pow(1+r[full]/100,part);
  return e;
}

/* estimated exit P/E: recent growth fades toward a 6% floor (3-year half-life);
   the premium of today's P/E over a 15x base shrinks with the excess growth that fades */
const PE_BASE=15, G_FLOOR=0.06, HALF=3;
function estPe(){
  const now=S.price/S.eps, n=S.n, g0=WTD;
  const gExit=G_FLOOR+(g0-G_FLOOR)*Math.pow(0.5,n/HALF);
  let pe, why;
  if(!(now>0)) return {pe:NaN,why:'Needs positive EPS.'};
  const h=v=>(Math.round(v*2)/2).toFixed(1);
  const TP=typPe();
  if(g0<=G_FLOOR&&now>TP*1.05){pe=TP; why=`Today the market pays ${now.toFixed(1)}× earnings, above the stock's ${typTxt()}. With earnings ${g0<0?'shrinking ('+pct(g0)+')':'growing slowly ('+pct(g0)+')'}, a P/E above the usual level rarely lasts, so the estimate uses that typical ${h(TP)}×.`;}
  else if(g0<=G_FLOOR||now<=PE_BASE){pe=now; why=g0<=G_FLOOR?`Today the market pays ${now.toFixed(1)}× earnings, close to the stock's ${typTxt()}. Growth is already slow (${pct(g0)}), so the estimate keeps today's P/E.`:`Today the market pays only ${now.toFixed(1)}× earnings, so the estimate keeps it rather than going lower.`;}
  else{pe=PE_BASE+(now-PE_BASE)*(gExit-G_FLOOR)/(g0-G_FLOOR); why=`Today the market pays ${now.toFixed(1)}× earnings. Investors pay more for fast growth, and growth usually slows. If growth slows from ${pct(g0)} now to about ${pct(gExit)} when you sell, the estimate is ${h(Math.max(6,pe))}×.`;}
  return {pe:Math.max(6,Math.round(pe*2)/2),why,gExit};
}
function peTrlMonths(){let m=Math.round((+S.peTrl||36)/3)*3; return Math.min(36,Math.max(6,m));}
/* typical P/E: median of quarter-end P/Es over the chosen months, with today's P/E as the last point */
function peTrailing(){const q=peTrlMonths()/3, v=[]; for(let i=L-q;i<=L;i++){const P=i===L?S.price:Q[i][2], E=i===L?S.eps:ttm[i]; if(P&&E>0) v.push(P/E);} v.sort((a,b)=>a-b); return v.length?v[Math.floor(v.length/2)]:NaN;}
const typPe=()=>peTrailing();
const typTxt=()=>`typical ${(Math.round(typPe()*2)/2).toFixed(1)}× (the median of the last ${peTrlMonths()} months)`;
function exitPe(){
  if(S.peMode==='est') return estPe().pe;
  if(S.peMode==='now') return Math.round(S.price/S.eps*2)/2;
  if(S.peMode==='trl') return Math.round(peTrailing()*2)/2;
  return S.pe;
}

const pct=(x,d=1)=>isFinite(x)?(x*100).toFixed(d)+'%':'n/a';

function tone(diff){return diff>0.015?'good':diff<-0.015?'bad':'warn';}

function defaultScen(pe){
  const g=isFinite(WTD)?WTD:0.05, r1=v=>Math.round(v*2)/2;
  return [{name:'Bear',g:r1((g-0.06)*100),pe:r1(Math.max(6,pe*0.75))},{name:'Base',g:r1(g*100),pe:r1(pe)},{name:'Bull',g:r1((g+0.06)*100),pe:r1(pe*1.2)}];
}

return { req, proj, reqPe, wtdAt, trajInfo, yearRates, yearlyAvg, trlMonths, trlGrowth, base, basePath, estPe, peTrlMonths, peTrailing, typPe, exitPe, tone, defaultScen };
}

// Line 936 (renderAll): the upper end of the exit P/E slider.
export function peSliderMax(ok, nowPe, pe) {
  const peMax=Math.max(45,Math.min(400,Math.ceil(Math.max(ok?nowPe:0,pe||0)*1.3/5)*5));
  return peMax;
}

// Line 721: share-count events. SH is the diluted share count per quarter.
export function shareEvents(SH) {
  const N = SH.length;
  const events=[]; for(let i=1;i<N;i++){const c=SH[i]/SH[i-1]-1; if(Math.abs(c)>=0.02) events.push({q:i,c});}
  return events;
}

// Lines 992-995 (enrich): 12-month changes and the P/E of each daily point.
// a holds {t, p, e, q}; t is a UTC timestamp in milliseconds.
export function enrich(a, ttm) {
  let j=0;
  a.forEach(d=>{const t=d.t-365*864e5; while(j<a.length-1&&a[j+1].t<=t) j++;
    d.r12=a[0].t<=t?d.p/a[j].p-1:null;
    d.g12=d.q>=7&&ttm[d.q-4]>0?ttm[d.q]/ttm[d.q-4]-1:null;
    d.pe=d.p/d.e;});
  return a;
}

// Line 998 and 999: the price-earnings link.
export function corr(xs,ys){const n=xs.length; if(n<20) return NaN; const mx=xs.reduce((a,b)=>a+b,0)/n, my=ys.reduce((a,b)=>a+b,0)/n; let sxy=0,sxx=0,syy=0; for(let i=0;i<n;i++){const dx=xs[i]-mx,dy=ys[i]-my;sxy+=dx*dy;sxx+=dx*dx;syy+=dy*dy;} return sxy/Math.sqrt(sxx*syy);}
export const corrWord=r=>!isFinite(r)?'':r<=-0.3?'negative':r>=0.6?'strong':r>=0.3?'moderate':'weak';

// Lines 1020-1022 (drawPx): the points of the chosen range.
export function rangePoints(all, px) {
  const end=all[all.length-1].t, months={'6M':6,'1Y':12,'3Y':36}[px]||36;
  const sd=new Date(end); sd.setUTCMonth(sd.getUTCMonth()-months);
  const D=all.filter(d=>d.t>=sd.getTime());
  return { D, end, start: sd.getTime() };
}

// Lines 1025, 1028-1029 (drawPx): the price axis and the overlay scales.
// fit returns the domain [a, b] instead of the pixel mapping built on it.
export function pxScales(D) {
  const ps=D.map(d=>d.p); let mn=Math.min(...ps)*0.9, mx=Math.max(...ps)*1.06; const st=niceStep(mx-mn,5); mn=Math.floor(mn/st)*st; mx=Math.ceil(mx/st)*st;
  const fit=(vals,pad=0.08)=>{vals=vals.filter(v=>v!=null&&isFinite(v)); if(!vals.length) return null; let a=Math.min(...vals),b=Math.max(...vals); const r=(b-a)||Math.abs(b)||1; a-=r*pad; b+=r*pad; return [a,b];};
  const Ye=fit(D.map(d=>d.e)), Ype=fit(D.map(d=>d.pe)), Yc=fit(D.flatMap(d=>[d.r12,d.g12]).concat([0]));
  return { price: [mn, mx, st], eps: Ye, pe: Ype, change: Yc, high: Math.max(...ps), low: Math.min(...ps) };
}

// Lines 1043-1048 and 1115 (drawPx, bandNear): the P/E bands.
export function bands(D) {
    const pes=D.map(d=>d.p/d.e).filter(v=>v>0&&isFinite(v)), pLo=Math.min(...pes)*0.9, pHi=Math.max(...pes)*1.05;
    let stp=1,best=1e9; for(let k=-1;k<=3;k++) for(const f of [1,2,2.5,5]){const c=f*Math.pow(10,k), n=Math.floor(pHi/c)-Math.ceil(pLo/c)+1; if(n<3) continue; const sc=Math.abs(n-5); if(sc<best){best=sc;stp=c;}}
    const ms=[]; for(let m=Math.ceil(pLo/stp)*stp; m<=pHi+1e-9; m+=stp) if(m>0) ms.push(+m.toFixed(2));
    const LST=D[D.length-1], nowPe=LST.p/LST.e, mu=ms.find(m=>m>=nowPe), md=[...ms].reverse().find(m=>m<=nowPe);
    const PXB={ms,nowPe,first:D[0].p/D[0].e,e:LST.e,p:LST.p};
    function bandNear(){ if(!PXB) return null; return PXB.ms.reduce((b,m)=>Math.abs(m-PXB.nowPe)<Math.abs(b-PXB.nowPe)?m:b,PXB.ms[0]); }
    return { ms, nowPe, mu, md, first: PXB.first, near: bandNear() };
}

// Line 1116 (bandCap): where a band's price today sits against today's price.
export function bandRelation(PXB, m) {
  const v=PXB.e*m;
  return Math.abs(v/PXB.p-1)<0.01?'right at':v>PXB.p?'above':'below';
}

// Lines 1154-1157 (drawRuler): the growth scale.
export function rulerScale(R, RECENT, WTD, LT, g) {
  const vals=[R,RECENT,WTD,LT,g].filter(isFinite);
  let lo=Math.min(0,...vals)-0.02, hi=Math.max(0.3,...vals)+0.03;
  lo=Math.floor(lo*20)/20; hi=Math.ceil(hi*20)/20;
  const step=(hi-lo)>0.8?0.2:(hi-lo)>0.4?0.1:0.05;
  return { lo, hi, step };
}

// Lines 1183-1192 (drawHeatmap): rows, columns, the marked rows and cells.
export function heatmap(R, g, pe, RECENT, WTD, LT, P, E, proj, n, D, r) {
  const vals=[R,RECENT,WTD,LT,g].filter(isFinite);
  let lo=Math.max(-0.4,Math.min(0,...vals)), hi=Math.min(1.5,Math.max(0.3,...vals));
  const step=[0.025,0.05,0.1,0.2].find(st=>(Math.ceil(hi/st)-Math.floor(lo/st))<=13)||0.2;
  const rows=[];for(let i=Math.floor(lo/step);i<=Math.ceil(hi/step);i++)rows.push(+(i*step).toFixed(4));
  const rnd=v=>v<40?Math.round(v):Math.round(v/5)*5;
  const cols=[...new Set([0.55,0.7,0.8,0.9,1,1.12,1.25,1.4,1.6].map(f=>f===1?pe:rnd(pe*f)))].sort((a,b)=>a-b);
  const near=(arr,v)=>arr.reduce((b,x,i)=>Math.abs(x-v)<Math.abs(arr[b]-v)?i:b,0);
  const selR=near(rows,g), selC=cols.indexOf(pe);
  const reqRow=isFinite(R)?near(rows,R):-1, recRow=near(rows,RECENT), wtdRow=near(rows,WTD), ltRow=isFinite(LT)?near(rows,LT):-1;
  const nowPe=P/E;
  // Lines 1193 and 1202-1204: the "today" column mark and each cell.
  const today=cols.map(c=>Math.abs(c-nowPe)<0.5);
  const cells=rows.map(gr=>cols.map(c=>{const p=proj(P,E,gr,n,c,D), diff=p.cagr-r; return { p, diff, z: diff>0.015?'good':diff<-0.015?'bad':'near' };}));
  return { step, rows, cols, selR, selC, reqRow, recRow, wtdRow, ltRow, today, cells, near };
}

// Lines 1244-1254 and 1263 (drawTraj): the paths, their scale and the shading.
export function traj(E, g, R, n, ttm, L, basePath) {
  const H0=6;
  const hist=[];for(let i=H0;i<=L;i++)hist.push(ttm[i]);
  const F=4*n, tot=hist.length-1+F;
  const Rg=isFinite(R)?Math.max(R,-0.99):0;
  const fut=k=>basePath(E,g,k), fr=k=>E*Math.pow(1+Rg,k/4);
  const vals=hist.concat([fut(F),fr(F),E]).filter(isFinite);
  let mn=Math.min(...vals)*0.92, mx=Math.max(...vals)*1.04;
  const st=niceStep(mx-mn,5); mn=Math.max(0,Math.floor(mn/st)*st);
  const shortfall=fr(F)>fut(F);
  return { hist, F, fut, fr, mn, mx, st, shortfall };
}

// Lines 1286-1293 (drawHist): required against delivered growth.
export function hist(S, Q, L, ttm, tyoy, pe, req) {
  const r=S.r/100,n=S.n,D=S.div; const H0=6;
  const idx=[];for(let i=H0;i<=L;i++)idx.push(i);
  const reqs=idx.map(i=>{const P=i===L?S.price:Q[i][2], E=i===L?S.eps:ttm[i]; return req(P,E,r,n,pe,D);});
  const del=idx.map(i=>tyoy[i]);
  const all=reqs.concat(del).filter(v=>v!=null&&isFinite(v));
  let mn=Math.min(0,...all), mx=Math.max(0.05,...all);
  const st=niceStep(mx-mn,5); mn=Math.floor(mn/st)*st; mx=Math.ceil(mx/st)*st;
  return { idx, reqs, del, mn, mx, st };
}

// Line 1241: the step between axis ticks.
export function niceStep(range,target){if(!(range>0))return 1;const raw=range/target,m=Math.pow(10,Math.floor(Math.log10(raw)));const f=raw/m;return (f<1.5?1:f<3?2:f<7?5:10)*m;}

// Lines 1352, 1359 and 1367 (drawFin): bar colors and scales. y is a change,
// i the quarter, M the metric (bad: a rise is bad).
export function finColor(k, y, i, FIN, M) {
  const col=(y,i)=>{ if(k==='opex'&&i!=null){const rv=FIN.rev[i]/FIN.rev[i-4]-1; return y>rv+0.005?'var(--warn)':'var(--muted)';} return (y>=0)!==M.bad?'var(--good)':'var(--bad)'; };
  return col(y, i);
}
export function finSharesScale(d) {
    let mn=Math.min(...d), mx=Math.max(...d); const pad=Math.max((mx-mn)*0.35,mx*0.004); mn-=pad; mx+=pad;
    return [mn, mx];
}
export function finBarsMax(d) {
    const mx=Math.max(...d)*1.12;
    return mx;
}

// Lines 1378-1383 (drawFin): the tiles. Returns the class of a tile.
export function tileClass(key, chg, FIN, M2, i, j) {
  const yo=(a,b)=>a/b-1;
  const good=key==='opex'?chg<=yo(FIN.rev[i],FIN.rev[j]):(chg>=0)!==M2.bad;
  const cls=key==='opex'?(chg>yo(FIN.rev[i],FIN.rev[j])+0.005?'warnc':'muted'):(Math.abs(chg)<1e-9?'muted':good?'up':'down');
  return cls;
}

// Lines 1398 and 1405-1406 (drawCmp): the compare series and their axis.
export function cmpSeries(d, idx) {
  return idx?d.map(x=>d[0]>0?x/d[0]-1:null):d.map((x,i)=>i&&d[i-1]>0?x/d[i-1]-1:null);
}
export function cmpScale(all) {
  let mn=Math.min(...all,0), mx=Math.max(...all,0);
  const st=niceStep(mx-mn||1,5); mn=Math.floor(mn/st)*st; mx=Math.ceil(mx/st)*st;
  return [mn, mx, st];
}

// Lines 1419-1422 (drawCmp): the quarter-to-quarter correlation table.
export function cmpChanges(raw) {
  return raw.map((v,i)=>i&&raw[i-1]>0?v/raw[i-1]-1:null).slice(1);
}
export const rr=(a,b)=>{const n=a.length,ma=a.reduce((s,v)=>s+v,0)/n,mb=b.reduce((s,v)=>s+v,0)/n;let sab=0,saa=0,sbb=0;for(let i=0;i<n;i++){sab+=(a[i]-ma)*(b[i]-mb);saa+=(a[i]-ma)**2;sbb+=(b[i]-mb)**2;}return saa&&sbb?sab/Math.sqrt(saa*sbb):NaN;};
export const word=r=>!isFinite(r)?'n/a':r>=0.6?'strong, same direction':r>=0.3?'moderate, same direction':r>-0.3?'weak or none':r>-0.6?'moderate, opposite directions':'strong, opposite directions';
