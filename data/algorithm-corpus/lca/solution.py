from collections import deque
n,q=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
L=n.bit_length(); up=[[0]*n for _ in range(L)]; dep=[-1]*n; dep[0]=0; queue=deque([0])
while queue:
    u=queue.popleft()
    for v in g[u]:
        if dep[v]<0: dep[v]=dep[u]+1; up[0][v]=u; queue.append(v)
for k in range(1,L):
    for v in range(n): up[k][v]=up[k-1][up[k-1][v]]
for _ in range(q):
    a,b=map(int,input().split()); a-=1; b-=1
    if dep[a]<dep[b]: a,b=b,a
    diff=dep[a]-dep[b]
    for k in range(L):
        if diff>>k&1: a=up[k][a]
    if a!=b:
        for k in range(L-1,-1,-1):
            if up[k][a]!=up[k][b]: a,b=up[k][a],up[k][b]
        a=up[0][a]
    print(a+1)
