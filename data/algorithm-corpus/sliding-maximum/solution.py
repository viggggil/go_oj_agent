from collections import deque
n,k=map(int,input().split()); a=list(map(int,input().split())); q=deque(); ans=[]
for i,x in enumerate(a):
    while q and q[0]<=i-k: q.popleft()
    while q and a[q[-1]]<=x: q.pop()
    q.append(i)
    if i>=k-1: ans.append(a[q[0]])
print(*ans)
