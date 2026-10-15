from collections import deque
n,m=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(m):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
c=[-1]*n; ok=True
for s in range(n):
    if c[s]>=0: continue
    c[s]=0; q=deque([s])
    while q:
        u=q.popleft()
        for v in g[u]:
            if c[v]<0: c[v]=c[u]^1; q.append(v)
            elif c[v]==c[u]: ok=False
print('YES' if ok else 'NO')
