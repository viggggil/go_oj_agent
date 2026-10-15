import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); bit=[0]*(n+1)
def add(i,x):
    while i<=n: bit[i]+=x; i+=i&-i
def pref(i):
    s=0
    while i: s+=bit[i]; i-=i&-i
    return s
for i in range(1,n+1): add(i,next(it))
for _ in range(q):
    op,l,r=next(it),next(it),next(it)
    if op==1: add(l,r)
    else: print(pref(r)-pref(l-1))
