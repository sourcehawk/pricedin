// Engine inputs built from the prototype's invented demo profiles. None of
// these numbers are reported results. The generators are copied from
// prototype/pricedin.html (lines 679-703 and 744-755) so that the profiles
// match what the prototype shows.

/* eslint-disable */

const N = 19; // Q1 22 .. Q3 26
// Line 681.
function rng(seed){return function(){seed|=0;seed=seed+0x6D2B79F5|0;let t=Math.imul(seed^seed>>>15,1|seed);t=t+Math.imul(t^t>>>7,61|t)^t;return((t^t>>>14)>>>0)/4294967296-0.5;};}
// Lines 682-697, without the label function.
function gen(p){
  const R=rng(p.seed), eps=new Array(N), sz=p.season.reduce((a,b)=>a+b,0);
  for(let i=N-4;i<N;i++) eps[i]=p.ttm*p.season[i%4]/sz*(1+R()*0.04);
  const k=p.ttm/eps.slice(N-4).reduce((a,b)=>a+b,0); for(let i=N-4;i<N;i++) eps[i]*=k;
  const g=i=>p.g0+(p.g1-p.g0)*(i-4)/(N-5);
  for(let i=N-5;i>=0;i--) eps[i]=eps[i+4]/(1+g(i+4)+R()*0.03);
  const ttmAt=i=>eps[i]+eps[i-1]+eps[i-2]+eps[i-3];
  const Q=eps.map((e,i)=>{let px=null; if(i>=6){px=(p.pe0+(p.pe1-p.pe0)*(i-6)/12)*(1+R()*0.1)*ttmAt(i);} if(i===N-1)px=p.price; return [i,+e.toFixed(3),px&&+px.toFixed(2)];});
  const SH=shareSeries(p.shares,p.trend,p.ev);
  const fin={rev:[],eps:[],op:[],fcf:[],shares:[]};
  for(let i=N-8;i<N;i++){
    const t=(i-(N-8))/7, ni=eps[i]*SH[i], rev=ni/p.nm, opm=p.opm0+(p.opm1-p.opm0)*t;
    fin.eps.push(+eps[i].toFixed(2)); fin.rev.push(Math.round(rev)); fin.op.push(Math.round(rev*opm)); fin.fcf.push(Math.round(ni*p.fcfc*(1+R()*0.5))); fin.shares.push(+SH[i].toFixed(1));
  }
  return {Q,fin,SH};
}
// Lines 699-703.
function shareSeries(base,trend,ev){
  const sh=new Array(N); sh[N-1]=base;
  for(let i=N-1;i>0;i--) sh[i-1]=sh[i]/(1+trend+((ev&&ev[i])?ev[i][0]:0));
  return sh;
}
// Line 744.
function qEnd(i){const y=2022+Math.floor(i/4), m=(i%4)*3+3; return Date.UTC(y,m,0);}
// Lines 745-755, with Q, L, ttm and the symbol as parameters.
function genDaily(sym, Q, L, ttm){
  const R=rng(sym.split('').reduce((a,c)=>a*31+c.charCodeAt(0),7)), out=[];
  for(let i=6;i<L;i++){
    const a=Q[i][2], b=Q[i+1][2], days=[];
    for(let t=qEnd(i)+864e5;t<=qEnd(i+1);t+=864e5){const wd=new Date(t).getUTCDay(); if(wd&&wd<6) days.push(t);}
    const n=days.length, W=[0]; for(let k=1;k<=n;k++) W.push(W[k-1]+R()*2*0.017);
    if(i===6) out.push({t:qEnd(6),p:a,e:ttm[6],q:6});
    days.forEach((t,k)=>{const f=(k+1)/n, br=W[k+1]-f*W[n]; const q=i+(k===n-1?1:0); out.push({t,p:Math.exp(Math.log(a)+(Math.log(b)-Math.log(a))*f+br),e:ttm[q],q});});
  }
  return out;
}

// Lines 705-714: the demo profiles that the cases use.
const GRMN_HAND = {
  Q:[['Q1 22',1.30,null],['Q2 22',1.45,null],['Q3 22',1.50,null],['Q4 22',1.50,null],['Q1 23',1.35,null],['Q2 23',1.65,null],['Q3 23',1.70,102],['Q4 23',1.85,128],['Q1 24',1.55,148],['Q2 24',2.00,162],['Q3 24',2.20,175],['Q4 24',2.45,205],['Q1 25',2.05,210],['Q2 25',2.30,205],['Q3 25',2.45,245],['Q4 25',3.05,225],['Q1 26',2.40,240],['Q2 26',2.60,255],['Q3 26',2.75,270]],
  fin:{rev:[1820,1540,1760,1776,2117,1810,1815,1990],eps:[2.45,2.05,2.30,2.45,3.05,2.40,2.60,2.75],op:[520,330,450,457,640,420,505,612],fcf:[380,360,160,425,470,390,210,293]},
  sh:{base:192,trend:0.0005,ev:{15:[-0.022,'Share buyback']}},
};
const GEN = {
  MSFT:{seed:11,ttm:14.0,price:520,g0:.10,g1:.16,pe0:34,pe1:37,season:[1,1,1,1.05],shares:7430,trend:-0.0025,ev:{},nm:.36,opm0:.45,opm1:.46,fcfc:1.0},
  AMD:{seed:37,ttm:3.0,price:165,g0:.05,g1:.45,pe0:70,pe1:55,season:[.9,1,1.05,1.1],shares:1620,trend:0.003,ev:{13:[0.04,'Shares issued']},nm:.12,opm0:.10,opm1:.14,fcfc:1.6},
  PLTR:{seed:41,ttm:0.60,price:180,g0:.30,g1:.80,pe0:120,pe1:300,season:[.9,.95,1.05,1.1],shares:2350,trend:0.01,ev:{9:[0.025,'Stock grants'],14:[0.02,'Stock grants']},nm:.28,opm0:.15,opm1:.30,fcfc:1.8},
  KO:{seed:67,ttm:2.95,price:70,g0:.07,g1:.05,pe0:23,pe1:24,season:[.95,1.1,1,.95],shares:4300,trend:-0.001,ev:{},nm:.25,opm0:.29,opm1:.30,fcfc:.9},
  NKE:{seed:79,ttm:2.10,price:70,g0:.10,g1:-.25,pe0:28,pe1:33,season:[1,1,1,1],shares:1480,trend:-0.006,ev:{16:[-0.025,'Accelerated buyback']},nm:.07,opm0:.12,opm1:.08,fcfc:1.2},
};

const label = (i) => `Q${(i % 4) + 1} FY${22 + Math.floor(i / 4)}`;
const iso = (t) => new Date(t).toISOString().slice(0, 10);
const M = 1e6;

// A v1 History for one demo profile. weekly keeps every fifth trading day
// to keep the cases file small; the example uses every day.
export function profile(sym, { weekly = false } = {}) {
  let Q, fin, SH;
  if (sym === "GRMN") {
    Q = GRMN_HAND.Q.map((q, i) => [i, q[1], q[2]]);
    fin = GRMN_HAND.fin;
    SH = shareSeries(GRMN_HAND.sh.base, GRMN_HAND.sh.trend, GRMN_HAND.sh.ev);
  } else {
    ({ Q, fin, SH } = gen(GEN[sym]));
  }
  const L = N - 1;
  const ttm = Q.map((q, i) => (i >= 3 ? Q[i][1] + Q[i - 1][1] + Q[i - 2][1] + Q[i - 3][1] : null));
  const quarters = Q.map((q, i) => {
    const f = i - (N - 8);
    const has = f >= 0;
    return {
      label: label(i),
      periodEnd: iso(qEnd(i)),
      eps: q[1],
      close: q[2] || null,
      dilutedShares: Math.round(SH[i] * M),
      revenue: has ? fin.rev[f] * M : null,
      operatingIncome: has ? fin.op[f] * M : null,
      operatingCosts: has ? (fin.rev[f] - fin.op[f]) * M : null,
      freeCashFlow: has ? fin.fcf[f] * M : null,
    };
  });
  let daily = genDaily(sym, Q, L, ttm).map((d) => ({ date: iso(d.t), close: +d.p.toFixed(4), ttmEps: +d.e.toFixed(4), ttmQuarter: label(d.q) }));
  if (weekly) daily = daily.filter((_, i) => i % 5 === 0 || i === daily.length - 1);
  const price = Q[L][2];
  const ttmEps = +Q.slice(N - 4).reduce((a, q) => a + q[1], 0).toFixed(2);
  return { history: { quarters, daily }, price, ttmEps };
}

// The prototype's default levers (line 723), in v1 units.
export function defaultLevers(p, over = {}) {
  return {
    price: p.price,
    ttmEps: p.ttmEps,
    dividend: 0,
    targetReturn: 0.15,
    holdingYears: 3,
    exitPe: { mode: "estimated", trailingMonths: 36 },
    baseline: { mode: "weighted", trailingMonths: 12 },
    scenarios: [],
    chartRange: "3Y",
    ...over,
  };
}
