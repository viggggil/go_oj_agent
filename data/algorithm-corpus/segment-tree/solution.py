import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); a=[next(it) for _ in range(n)]; tr=[0]*(4*n); lazy=[0]*(4*n)
def build(p,l,r):
    if l==r: tr[p]=a[l]; return
    m=(l+r)//2; build(p*2,l,m); build(p*2+1,m+1,r); tr[p]=tr[p*2]+tr[p*2+1]
def apply(p,l,r,x): tr[p]+=x*(r-l+1); lazy[p]+=x
def push(p,l,r):
    if lazy[p] and l<r:
        m=(l+r)//2; apply(p*2,l,m,lazy[p]); apply(p*2+1,m+1,r,lazy[p]); lazy[p]=0
def change(p,l,r,ql,qr,x):
    if ql<=l and r<=qr: apply(p,l,r,x); return
    push(p,l,r); m=(l+r)//2
    if ql<=m: change(p*2,l,m,ql,qr,x)
    if qr>m: change(p*2+1,m+1,r,ql,qr,x)
    tr[p]=tr[p*2]+tr[p*2+1]
def query(p,l,r,ql,qr):
    if ql<=l and r<=qr: return tr[p]
    push(p,l,r); m=(l+r)//2; ans=0
    if ql<=m: ans+=query(p*2,l,m,ql,qr)
    if qr>m: ans+=query(p*2+1,m+1,r,ql,qr)
    return ans
build(1,0,n-1)
for _ in range(q):
    op,l,r=next(it),next(it)-1,next(it)-1
    if op==1: change(1,0,n-1,l,r,next(it))
    else: print(query(1,0,n-1,l,r))
